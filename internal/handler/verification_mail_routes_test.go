package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	emailworker "github.com/perfect-panel/server/queue/logic/email"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

type verificationMail struct {
	from, to string
	message  []byte
}

func receiveVerificationMail(listener net.Listener) (verificationMail, error) {
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

func TestRegistrationCodeInDeliveredEmailCanBeVerified(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	inbox := make(chan verificationMail, 1)
	relayResult := make(chan error, 1)
	go func() {
		delivery, err := receiveVerificationMail(listener)
		inbox <- delivery
		relayResult <- err
	}()

	inspector := f.startQueue()
	f.service.AuthLimiter = limit.NewPeriodLimit(86400, 10, f.service.Redis, "fixture:mail:")
	f.service.Config.Site.SiteName = "Subscription portal"
	f.service.Config.Email.Platform = "smtp"
	f.service.Config.Email.PlatformConfig = fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"from":"portal@example.test"}`, listener.Addr().(*net.TCPAddr).Port)
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
	select {
	case err := <-relayResult:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("verification email was not delivered")
	}
	delivery := <-inbox
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
