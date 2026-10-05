package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// telegramBindTokenTTL bounds how long a deep link stays usable. The value is
// the expiry advertised to the client, so the two can no longer drift.
const telegramBindTokenTTL = 300 * time.Second

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
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || u == nil {
		l.Logger.Errorw("bind telegram failed: user missing from context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if l.svcCtx.Config.Telegram.BotName == "" {
		l.Logger.Errorw("bind telegram failed: telegram bot is not initialized")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "telegram bot is not configured")
	}

	// The deep link carries a dedicated single-use token rather than the
	// caller's session id: the link travels through Telegram chats and
	// screenshots, and a leaked session id would let its holder bind their
	// own Telegram account — and therefore log in — as this user.
	token := random.KeyNew(32, 1)
	expiredAt := time.Now().Add(telegramBindTokenTTL)
	key := fmt.Sprintf("%s:%s", config.TelegramBindKey, token)
	if err := l.svcCtx.Redis.Set(l.ctx, key, u.Id, telegramBindTokenTTL).Err(); err != nil {
		l.Logger.Errorw("bind telegram failed: cannot store bind token",
			zap.Any("user_id", u.Id),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "store telegram bind token failed: %v", err)
	}

	return &types.BindTelegramResponse{
		Url:       fmt.Sprintf("https://t.me/%s?start=%s", l.svcCtx.Config.Telegram.BotName, token),
		ExpiredAt: expiredAt.UnixMilli(),
	}, nil
}
