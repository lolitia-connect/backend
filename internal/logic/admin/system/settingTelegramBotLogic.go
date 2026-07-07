package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"

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
	initialize.Telegram(l.svcCtx)
	return nil
}
