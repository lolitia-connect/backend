package common

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
)

func TestVerificationDeliveryQueuesMatchingCodesAndThrottlesResends(t *testing.T) {
	ctx := t.Context()
	db := newPublicConfigDB(t)
	cache := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	queueOptions := asynq.RedisClientOpt{Addr: cache.Addr()}
	delivery := asynq.NewClient(queueOptions)
	inspector := asynq.NewInspector(queueOptions)
	t.Cleanup(func() { _ = inspector.Close(); _ = delivery.Close(); _ = rds.Close() })
	service := &svc.ServiceContext{
		Store: repository.NewStore(db, rds), Redis: rds, Queue: delivery,
		AuthLimiter: limit.NewPeriodLimit(86400, 10, rds, "test:verification:"),
	}
	service.Config.Model = constant.DevMode
	service.Config.VerifyCode.ExpireTime = 180
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
	cache.FastForward(181 * time.Second)
	verify(emailCheck, true)
	verify(smsCheck, false)
	cache.FastForward(120 * time.Second)
	verify(emailCheck, false)
}
