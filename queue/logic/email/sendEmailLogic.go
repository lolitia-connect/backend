package emailLogic

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/queue/types"
)

type SendEmailLogic struct {
	svcCtx *svc.ServiceContext
}

func NewSendEmailLogic(svcCtx *svc.ServiceContext) *SendEmailLogic {
	return &SendEmailLogic{
		svcCtx: svcCtx,
	}
}

func renderEmailTemplate(name, text string, data map[string]interface{}) (string, error) {
	tpl, err := template.New(name).Parse(text)
	if err != nil {
		return "", err
	}
	var result bytes.Buffer
	if err := tpl.Execute(&result, data); err != nil {
		return "", err
	}
	return result.String(), nil
}

// resolveSubject prefers the operator-configured subject over the fallback
// literal the producer queued. The configured subject renders with the same
// data as the body; if it fails to render it is still sent as raw text,
// because a localized subject with a template typo beats silently reverting
// to English.
func resolveSubject(configured, fallback string, data map[string]interface{}) string {
	if configured == "" {
		return fallback
	}
	rendered, err := renderEmailTemplate("subject", configured, data)
	if err != nil {
		zap.S().Error("[SendEmailLogic] Execute subject template failed",
			zap.Any("error", err.Error()),
			zap.Any("subject", configured),
		)
		return configured
	}
	return rendered
}
func (l *SendEmailLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload types.SendEmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		zap.S().Error("[SendEmailLogic] Unmarshal payload failed",
			zap.Any("error", err.Error()),
			zap.Any("payload", task.Payload()),
		)
		return nil
	}
	messageLog := log.Message{
		Metadata: payload.Metadata,
		Platform: l.svcCtx.Config.Email.Platform,
		To:       payload.Email,
		Subject:  payload.Subject,
		Content:  payload.Content,
	}
	sender, err := email.NewSender(l.svcCtx.Config.Email.Platform, l.svcCtx.Config.Email.PlatformConfig, l.svcCtx.Config.Site.SiteName)
	if err != nil {
		zap.S().Error("[SendEmailLogic] NewSender failed", zap.Any("error", err.Error()))
		return nil
	}
	// The operator-configured subject of a typed notification wins over the
	// literal queued by the producer; it renders with the same data as the
	// body so subjects can interpolate {{.SiteName}} and friends.
	var content, bodyTemplate, subjectTemplate string
	switch payload.Type {
	case types.EmailTypeVerify:
		payload.Content["Type"] = uint8(payload.Content["Type"].(float64))
		bodyTemplate = l.svcCtx.Config.Email.VerifyEmailTemplate
		subjectTemplate = l.svcCtx.Config.Email.VerifyEmailSubject
	case types.EmailTypeMaintenance:
		bodyTemplate = l.svcCtx.Config.Email.MaintenanceEmailTemplate
		subjectTemplate = l.svcCtx.Config.Email.MaintenanceEmailSubject
	case types.EmailTypeExpiration:
		bodyTemplate = l.svcCtx.Config.Email.ExpirationEmailTemplate
		subjectTemplate = l.svcCtx.Config.Email.ExpirationEmailSubject
	case types.EmailTypeTrafficExceed:
		bodyTemplate = l.svcCtx.Config.Email.TrafficExceedEmailTemplate
		subjectTemplate = l.svcCtx.Config.Email.TrafficExceedEmailSubject
	case types.EmailTypeCustom:
		if payload.Content == nil {
			zap.S().Error("[SendEmailLogic] Custom email content is empty",
				zap.Any("payload", payload),
			)
			return nil
		}
		if tpl, ok := payload.Content["content"].(string); !ok {
			zap.S().Error("[SendEmailLogic] Custom email content is not a string",
				zap.Any("payload", payload),
			)
			return nil
		} else {
			content = tpl
		}
	default:
		zap.S().Error("[SendEmailLogic] Unsupported email type",
			zap.Any("type", payload.Type),
			zap.Any("payload", payload),
		)
		return nil
	}
	if bodyTemplate != "" {
		rendered, renderErr := renderEmailTemplate(payload.Type, bodyTemplate, payload.Content)
		if renderErr != nil {
			zap.S().Error("[SendEmailLogic] Execute template failed",
				zap.Any("error", renderErr.Error()),
				zap.Any("template", bodyTemplate),
				zap.Any("data", payload.Content),
			)
			return nil
		}
		content = rendered
	}
	subject := resolveSubject(subjectTemplate, payload.Subject, payload.Content)
	messageLog.Subject = subject

	err = sender.Send([]string{payload.Email}, subject, content)
	if err != nil {
		zap.S().Error("[SendEmailLogic] Send email failed", zap.Any("error", err.Error()))
		return nil
	}
	messageLog.Status = 1
	emailLog, err := messageLog.Marshal()
	if err != nil {
		zap.S().Error("[SendEmailLogic] Marshal message log failed",
			zap.Any("error", err.Error()),
			zap.Any("messageLog", messageLog),
		)
		return nil
	}

	if err = l.svcCtx.Store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeEmailMessage.Uint8(),
		Date:     time.Now().Format("2006-01-02"),
		ObjectID: 0,
		Content:  string(emailLog),
	}); err != nil {
		zap.S().Error("[SendEmailLogic] Insert email log failed",
			zap.Any("error", err.Error()),
			zap.Any("emailLog", string(emailLog)),
		)
		return nil
	}
	return nil
}
