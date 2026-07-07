package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BindTelegramLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Bind Telegram
func NewBindTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindTelegramLogic {
	return &BindTelegramLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BindTelegramLogic) BindTelegram() (resp *types.BindTelegramResponse, err error) {
	session, ok := l.ctx.Value(constant.CtxKeySessionID).(string)
	if !ok || session == "" {
		l.Logger.Errorw("bind telegram failed: session id missing from context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if l.svcCtx.Config.Telegram.BotName == "" {
		l.Logger.Errorw("bind telegram failed: telegram bot is not initialized")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "telegram bot is not configured")
	}
	return &types.BindTelegramResponse{
		Url:       fmt.Sprintf("https://t.me/%s?start=%s", l.svcCtx.Config.Telegram.BotName, session),
		ExpiredAt: time.Now().Add(300 * time.Second).UnixMilli(),
	}, nil
}
