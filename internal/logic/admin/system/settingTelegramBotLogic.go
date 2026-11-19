package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	modellog "github.com/perfect-panel/server/internal/model/log"

	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type SettingTelegramBotLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSettingTelegramBotLogic setting telegram bot
func NewSettingTelegramBotLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SettingTelegramBotLogic {
	return &SettingTelegramBotLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SettingTelegramBotLogic) SettingTelegramBot() error {
	if err := initialize.Telegram(l.svcCtx); err != nil {
		return err
	}
	modellog.RecordAdminAction(l.ctx, l.svcCtx.Store.Log(), modellog.AdminAction{Action: "settings.telegram.reload", Object: "telegram"})
	return nil
}
