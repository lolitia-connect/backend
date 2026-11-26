package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func verificationService(t *testing.T, dailyLimit int) (*svc.ServiceContext, *miniredis.Miniredis, *asynq.Inspector) {
	t.Helper()
	db := newPublicConfigDB(t)
	cache := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	queueOptions := asynq.RedisClientOpt{Addr: cache.Addr()}
	delivery := asynq.NewClient(queueOptions)
	inspector := asynq.NewInspector(queueOptions)
	t.Cleanup(func() { _ = inspector.Close(); _ = delivery.Close(); _ = rds.Close() })
	service := &svc.ServiceContext{
		Store: repository.NewStore(db, rds), Redis: rds, Queue: delivery,
		AuthLimiter: limit.NewPeriodLimit(86400, dailyLimit, rds, "test:verification:"),
	}
	service.Config.Model = constant.DevMode
	service.Config.VerifyCode.ExpireTime = 180
	return service, cache, inspector
}

func TestVerificationDeliveryWaitsForAnEligibleRecipientAndAvailableQueue(t *testing.T) {
	ctx := t.Context()
	service, cache, inspector := verificationService(t, 20)
	db := service.Store.Ent()
	const email, phone = "member@example.test", "4155552671"
	request := func(purpose uint8, want uint32) {
		t.Helper()
		mail, mailErr := NewSendEmailCodeLogic(ctx, service).SendEmailCode(&types.SendCodeRequest{Email: email, Type: purpose})
		sms, smsErr := NewSendSmsCodeLogic(ctx, service).SendSmsCode(&types.SendSmsCodeRequest{TelephoneAreaCode: "1", Telephone: phone, Type: purpose})
		if want == 200 {
			require.NoError(t, mailErr)
			require.NoError(t, smsErr)
			require.True(t, mail.Status && sms.Status)
			return
		}
		for _, err := range []error{mailErr, smsErr} {
			var code *xerr.CodeError
			require.ErrorAs(t, err, &code)
			require.Equal(t, want, code.GetErrCode())
		}
		require.Nil(t, mail)
		require.Nil(t, sms)
	}
	security := uint8(constant.Security)
	request(security, xerr.UserNotExist)
	account := db.User.Create().SetPassword("fixture").SaveX(ctx)
	for method, id := range map[string]string{"email": email, "mobile": "+1" + phone} {
		db.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType(method).SetAuthIdentifier(id).SetVerified(true).SaveX(ctx)
	}
	request(uint8(constant.Register), xerr.UserExist)
	offline := true
	db.UserAuthMethod.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if offline {
				return nil, errors.New("recipient directory unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	cache.FastForward(time.Minute + time.Second)
	request(security, xerr.DatabaseQueryError)
	offline = false
	delivery := service.Queue
	unavailable := asynq.NewClient(asynq.RedisClientOpt{Addr: cache.Addr()})
	require.NoError(t, unavailable.Close())
	service.Queue = unavailable
	cache.FastForward(time.Minute + time.Second)
	request(security, xerr.ERROR)
	queues, err := inspector.Queues()
	require.NoError(t, err)
	require.Empty(t, queues)
	service.Queue = delivery
	cache.FastForward(time.Minute + time.Second)
	request(security, 200)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	kinds := []string{jobs[0].Type, jobs[1].Type}
	require.ElementsMatch(t, []string{queue.ForthwithSendEmail, queue.ForthwithSendSms}, kinds)
}

func TestVerificationDeliveryQueuesMatchingCodesAndThrottlesResends(t *testing.T) {
	ctx := t.Context()
	service, cache, inspector := verificationService(t, 1)
	register := uint8(constant.Register)
	emailReq := &types.SendCodeRequest{Email: "new@example.test", Type: register}
	emailLogic := NewSendEmailCodeLogic(ctx, service)
	smsReq := &types.SendSmsCodeRequest{TelephoneAreaCode: "1", Telephone: "4155552671", Type: register}
	smsLogic := NewSendSmsCodeLogic(ctx, service)
	email, err := emailLogic.SendEmailCode(emailReq)
	if err != nil || !email.Status || len(email.Code) != 6 {
		t.Fatalf("email delivery status = %+v, %v", email, err)
	}
	sms, err := smsLogic.SendSmsCode(smsReq)
	if err != nil || !sms.Status || len(sms.Code) != 6 {
		t.Fatalf("SMS delivery status = %+v, %v", sms, err)
	}
	_, emailRepeat := emailLogic.SendEmailCode(emailReq)
	_, smsRepeat := smsLogic.SendSmsCode(smsReq)
	for _, got := range []error{emailRepeat, smsRepeat} {
		var code *xerr.CodeError
		if !errors.As(got, &code) || code.GetErrCode() != xerr.TooManyRequests {
			t.Fatalf("repeat was not throttled: %v", got)
		}
	}
	pending, err := inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending deliveries = %d, %v", len(pending), err)
	}
	seen := map[string]bool{}
	for _, task := range pending {
		seen[task.Type] = true
		switch task.Type {
		case queue.ForthwithSendEmail:
			var payload queue.SendEmailPayload
			if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.Email != emailReq.Email || payload.Content["Code"] != email.Code {
				t.Fatalf("queued email does not match request: %v", err)
			}
		case queue.ForthwithSendSms:
			var payload queue.SendSmsPayload
			if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.TelephoneArea != "1" || payload.Telephone != smsReq.Telephone || payload.Content != sms.Code {
				t.Fatalf("queued SMS does not match request: %v", err)
			}
		}
	}
	if !seen[queue.ForthwithSendEmail] || !seen[queue.ForthwithSendSms] {
		t.Fatalf("delivery types = %v", seen)
	}

	check := NewCheckVerificationCodeLogic(ctx, service)
	emailCheck := types.CheckVerificationCodeRequest{Method: "email", Account: emailReq.Email, Type: register, Code: email.Code}
	smsCheck := types.CheckVerificationCodeRequest{Method: "mobile", Account: "1" + smsReq.Telephone, Type: register, Code: sms.Code}
	verify := func(req types.CheckVerificationCodeRequest, want bool) {
		t.Helper()
		got, err := check.CheckVerificationCode(&req)
		if err != nil || got.Status != want {
			t.Fatalf("%s verified = %+v, %v (want %t)", req.Method, got, err, want)
		}
	}
	verify(emailCheck, true)
	verify(smsCheck, true)
	mismatch := emailCheck
	mismatch.Code = "not-the-code"
	verify(mismatch, false)
	cache.FastForward(61 * time.Second)
	_, emailDaily := emailLogic.SendEmailCode(emailReq)
	_, smsDaily := smsLogic.SendSmsCode(smsReq)
	for _, got := range []error{emailDaily, smsDaily} {
		var code *xerr.CodeError
		if !errors.As(got, &code) || code.GetErrCode() != xerr.TodaySendCountExceedsLimit {
			t.Fatalf("resend after the short cooldown = %v, want daily limit", got)
		}
	}
	pending, err = inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 2 {
		t.Fatalf("daily limit left %d queued deliveries, want the two originals: %v", len(pending), err)
	}
	verify(emailCheck, true)
	verify(smsCheck, true)
	cache.FastForward(120 * time.Second)
	verify(emailCheck, true)
	verify(smsCheck, false)
	cache.FastForward(120 * time.Second)
	verify(emailCheck, false)
	cache.FastForward(24 * time.Hour)
	email, err = emailLogic.SendEmailCode(emailReq)
	if err != nil {
		t.Fatal(err)
	}
	sms, err = smsLogic.SendSmsCode(smsReq)
	if err != nil {
		t.Fatal(err)
	}
	emailCheck.Code, smsCheck.Code = email.Code, sms.Code
	verify(emailCheck, true)
	verify(smsCheck, true)
	pending, err = inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 4 {
		t.Fatalf("new daily window left %d queued deliveries, want four: %v", len(pending), err)
	}
}
