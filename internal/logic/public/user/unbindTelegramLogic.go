package user

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/server/pkg/constant"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UnbindTelegramLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Unbind Telegram
func NewUnbindTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbindTelegramLogic {
	return &UnbindTelegramLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbindTelegramLogic) UnbindTelegram() error {
	// Get User Info
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)

	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	method, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, u.Id, "telegram")
	if err != nil {
		l.Logger.Errorw("UnbindTelegramLogic FindUserAuthMethodByPlatform Error", zap.Any("id", u.Id), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find User Auth Method By Platform Failed")
	}

	userTelegramChatId, err := strconv.ParseInt(method.AuthIdentifier, 10, 64)
	if err != nil {
		l.Logger.Errorw("UnbindTelegramLogic ParseInt Error", zap.Any("id", u.Id), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ParseInt Error")
	}

	if userTelegramChatId == 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.TelegramNotBound), "Unbind Telegram")
	}

	// Unbind Telegram
	err = l.svcCtx.Store.User().DeleteUserAuthMethods(l.ctx, u.Id, "telegram")
	if err != nil {
		l.Logger.Errorw("UnbindTelegramLogic DeleteUserAuthMethods Error", zap.Any("id", u.Id), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "Delete User Auth Methods Failed")
	}
	// Unbind Telegram Success send message with chatId
	text, err := tool.RenderTemplateToString(telegram.UnbindNotify, map[string]string{
		"Id":   strconv.FormatInt(u.Id, 10),
		"Time": time.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		l.Logger.Errorw("UnbindTelegramLogic RenderTemplateToString Error", zap.Any("id", u.Id), zap.Any("error", err.Error()))
		return nil
	}
	if l.svcCtx.TelegramBot == nil {
		l.Logger.Errorw("UnbindTelegramLogic TelegramBot is nil", zap.Any("id", u.Id))
		return nil
	}
	msg := tgbotapi.NewMessage(userTelegramChatId, text)
	_, err = l.svcCtx.TelegramBot.Send(msg)
	if err != nil {
		l.Logger.Errorw("UnbindTelegramLogic Send Error", zap.Any("id", u.Id), zap.Any("error", err.Error()))
		return nil
	}
	return nil
}
