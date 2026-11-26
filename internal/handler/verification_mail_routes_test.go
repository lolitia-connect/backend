package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/xerr"
	emailworker "github.com/perfect-panel/server/queue/logic/email"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

type verificationMail struct {
	from, to string
	message  []byte
}

func receiveVerificationMail(listener net.Listener, refused []string) (verificationMail, error) {
	var delivery verificationMail
	conn, err := listener.Accept()
	if err != nil {
		return delivery, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return delivery, err
	}
	wire := textproto.NewConn(conn)
	if err := wire.PrintfLine("220 test ESMTP"); err != nil {
		return delivery, err
	}
	for {
		line, err := wire.ReadLine()
		if err != nil {
			return delivery, err
		}
		response := "250 OK"
		switch {
		case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "):
		case strings.HasPrefix(line, "MAIL FROM:"):
			delivery.from = strings.TrimPrefix(line, "MAIL FROM:")
		case strings.HasPrefix(line, "RCPT TO:"):
			delivery.to = strings.TrimPrefix(line, "RCPT TO:")
			if slices.Contains(refused, strings.Trim(delivery.to, "<>")) {
				return delivery, wire.PrintfLine("550 Mailbox unavailable")
			}
		case line == "DATA":
			if err := wire.PrintfLine("354 End with a dot"); err != nil {
				return delivery, err
			}
			delivery.message, err = wire.ReadDotBytes()
			if err != nil {
				return delivery, err
			}
		case line == "QUIT":
			return delivery, wire.PrintfLine("221 Bye")
		default:
			return delivery, fmt.Errorf("unexpected SMTP command %q", line)
		}
		if err := wire.PrintfLine("%s", response); err != nil {
			return delivery, err
		}
	}
}

func captureMail(t *testing.T, f *routeFixture, refused ...string) func() []verificationMail {
	t.Helper()
	config, finish := startMailRelay(t, refused...)
	f.service.Config.Email.Platform = "smtp"
	f.service.Config.Email.PlatformConfig = string(config)
	return finish
}

func startMailRelay(t *testing.T, refused ...string) (json.RawMessage, func() []verificationMail) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	config := json.RawMessage(fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"from":"portal@example.test"}`, listener.Addr().(*net.TCPAddr).Port))
	var inbox []verificationMail
	var relayErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			delivery, err := receiveVerificationMail(listener, refused)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					relayErr = err
				}
				return
			}
			inbox = append(inbox, delivery)
		}
	}()
	finish := func() []verificationMail {
		t.Helper()
		_ = listener.Close()
		select {
		case <-done:
			require.NoError(t, relayErr)
		case <-time.After(12 * time.Second):
			t.Fatal("SMTP relay did not finish")
		}
		return inbox
	}
	t.Cleanup(func() { finish() })
	return config, finish
}

func TestOperatorDeliveryCheckAndQueuedNoticesUseConfiguredRelay(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	_, member := f.account(false)
	const editor = "/v1/admin/auth-method"
	const probe = editor + "/test_email_send"
	const recipient = "member@example.test"
	const probeForm = `{"email":"operator@example.test"}`
	f.request(http.MethodPost, probe, member, probeForm, xerr.InvalidAccess)
	f.request(http.MethodPost, probe, admin, `{}`, xerr.InvalidParams)
	f.request(http.MethodPost, probe, admin, probeForm, xerr.ERROR)
	var catalog types.PlatformResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, editor+"/email_platform", admin, "", http.StatusOK), &catalog))
	i := slices.IndexFunc(catalog.List, func(p types.PlatformInfo) bool { return p.Platform == "smtp" })
	require.NotEqual(t, -1, i, "the operator must be able to select SMTP")
	relay, inbox := startMailRelay(t, "retired@example.test")
	method := f.client.AuthMethod.Create().SetMethod("email").SetConfig(`{}`).SetEnabled(false).SaveX(ctx)
	settings := map[string]any{
		"platform": catalog.List[i].Platform, "platform_config": relay,
		"maintenance_email_subject": "Maintenance for {{.Plan}}", "maintenance_email_template": "Offline at {{.When}}",
		"expiration_email_subject": "Renew {{.Plan}}", "expiration_email_template": "Expires at {{.When}}",
		"traffic_exceed_email_subject": "{{.Plan}} usage", "traffic_exceed_email_template": "Limit reached at {{.When}}",
	}
	form, err := json.Marshal(map[string]any{"id": fmt.Sprint(method.ID), "method": "email", "enabled": true, "config": settings})
	require.NoError(t, err)
	f.request(http.MethodPut, editor+"/config", admin, string(form), http.StatusOK)
	f.request(http.MethodPost, probe, admin, `{"email":"retired@example.test"}`, http.StatusInternalServerError)
	f.request(http.MethodPost, probe, admin, probeForm, http.StatusOK)
	worker := emailworker.NewSendEmailLogic(f.service)
	notices := []struct{ kind, subject, body string }{
		{queue.EmailTypeMaintenance, "Maintenance for Monthly", "Offline at noon"},
		{queue.EmailTypeExpiration, "Renew Monthly", "Expires at noon"},
		{queue.EmailTypeTrafficExceed, "Monthly usage", "Limit reached at noon"},
		{queue.EmailTypeCustom, "Account notice", "custom message"},
	}
	for i, notice := range notices {
		payload, err := json.Marshal(queue.SendEmailPayload{Type: notice.kind, Email: recipient, Subject: "Account notice",
			Content: map[string]any{"Plan": "Monthly", "When": "noon", "content": "custom message"}})
		require.NoError(t, err)
		task := asynq.NewTask(queue.ForthwithSendEmail, payload)
		if i == 0 {
			interrupted, cancel := context.WithCancel(ctx)
			cancel()
			require.ErrorIs(t, worker.ProcessTask(interrupted, task), context.Canceled)
			require.Zero(t, f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeEmailMessage.Uint8())).CountX(ctx))
		}
		require.NoError(t, worker.ProcessTask(ctx, task))
	}
	deliveries := inbox()
	require.Len(t, deliveries, 2+len(notices))
	require.Equal(t, "<retired@example.test>", deliveries[0].to)
	require.Empty(t, deliveries[0].message)
	require.Equal(t, "<operator@example.test>", deliveries[1].to)
	receipts := f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeEmailMessage.Uint8())).Order(systemlog.ByID()).AllX(ctx)
	require.Len(t, receipts, len(notices))
	for i, notice := range notices {
		delivery := deliveries[i+2]
		require.Equal(t, "<"+recipient+">", delivery.to)
		msg, err := mail.ReadMessage(bytes.NewReader(delivery.message))
		require.NoError(t, err)
		require.Equal(t, notice.subject, msg.Header.Get("Subject"))
		body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
		require.NoError(t, err)
		require.Equal(t, notice.body, strings.TrimSpace(string(body)))
		var receipt log.Message
		require.NoError(t, json.Unmarshal([]byte(receipts[i].Content), &receipt))
		require.Equal(t, recipient, receipt.To)
		require.Equal(t, notice.subject, receipt.Subject)
		require.EqualValues(t, 1, receipt.Status)
	}
}

func TestRegistrationCodeInDeliveredEmailCanBeVerified(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inbox := captureMail(t, f)
	inspector := f.startQueue()
	f.service.AuthLimiter = limit.NewPeriodLimit(86400, 10, f.service.Redis, "fixture:mail:")
	f.service.Config.Site.SiteName = "Subscription portal"
	f.service.Config.Email.VerifyEmailSubject = "{{.SiteName}} registration"
	f.service.Config.Email.VerifyEmailTemplate = "Your registration code is {{.Code}}"
	const address = "recipient@example.test"
	var sent types.SendCodeResponse
	data := f.request(http.MethodPost, "/v1/common/send_code", "", fmt.Sprintf(`{"email":%q,"type":%d}`, address, constant.Register), http.StatusOK)
	require.NoError(t, json.Unmarshal(data, &sent))
	require.True(t, sent.Status)
	require.Empty(t, sent.Code)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, queue.ForthwithSendEmail, jobs[0].Type)
	require.NoError(t, emailworker.NewSendEmailLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
	deliveries := inbox()
	require.Len(t, deliveries, 1)
	delivery := deliveries[0]
	require.Equal(t, "<portal@example.test>", delivery.from)
	require.Equal(t, "<"+address+">", delivery.to)
	message, err := mail.ReadMessage(bytes.NewReader(delivery.message))
	require.NoError(t, err)
	require.Equal(t, "Subscription portal registration", message.Header.Get("Subject"))
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	require.NoError(t, err)
	text := strings.TrimSpace(string(body))
	code, ok := strings.CutPrefix(text, "Your registration code is ")
	require.True(t, ok, "unexpected rendered verification email")
	require.Len(t, code, 6)
	var verified types.CheckVerificationCodeRespone
	data = f.request(http.MethodPost, "/v1/common/check_verification_code", "", fmt.Sprintf(`{"method":"email","account":%q,"type":%d,"code":%q}`, address, constant.Register, code), http.StatusOK)
	require.NoError(t, json.Unmarshal(data, &verified))
	require.True(t, verified.Status)
	entry := f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeEmailMessage.Uint8())).OnlyX(ctx)
	var receipt log.Message
	require.NoError(t, json.Unmarshal([]byte(entry.Content), &receipt))
	require.Equal(t, address, receipt.To)
	require.Equal(t, message.Header.Get("Subject"), receipt.Subject)
	require.EqualValues(t, 1, receipt.Status)
}
