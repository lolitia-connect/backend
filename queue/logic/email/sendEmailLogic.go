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
	var content string
	switch payload.Type {
	case types.EmailTypeVerify:
		tpl, _ := template.New("verify").Parse(l.svcCtx.Config.Email.VerifyEmailTemplate)
		var result bytes.Buffer

		payload.Content["Type"] = uint8(payload.Content["Type"].(float64))

		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			zap.S().Error("[SendEmailLogic] Execute template failed",
				zap.Any("error", err.Error()),
				zap.Any("data", payload.Content),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeMaintenance:
		tpl, _ := template.New("maintenance").Parse(l.svcCtx.Config.Email.MaintenanceEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			zap.S().Error("[SendEmailLogic] Execute template failed",
				zap.Any("error", err.Error()),
				zap.Any("template", l.svcCtx.Config.Email.MaintenanceEmailTemplate),
				zap.Any("data", payload.Content),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeExpiration:
		tpl, _ := template.New("expiration").Parse(l.svcCtx.Config.Email.ExpirationEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			zap.S().Error("[SendEmailLogic] Execute template failed",
				zap.Any("error", err.Error()),
				zap.Any("template", l.svcCtx.Config.Email.ExpirationEmailTemplate),
				zap.Any("data", payload.Content),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeTrafficExceed:
		tpl, _ := template.New("traffic_exceed").Parse(l.svcCtx.Config.Email.TrafficExceedEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			zap.S().Error("[SendEmailLogic] Execute template failed",
				zap.Any("error", err.Error()),
				zap.Any("template", l.svcCtx.Config.Email.TrafficExceedEmailTemplate),
				zap.Any("data", payload.Content),
			)
			return nil
		}
		content = result.String()
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

	err = sender.Send([]string{payload.Email}, payload.Subject, content)
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
