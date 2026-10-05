package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

// upgradePasswordAfterLogin transparently re-hashes a verified plaintext
// password to the current algorithm/parameters when the stored hash is stale.
// Failures are logged and ignored so that a successful login is never blocked.
func upgradePasswordAfterLogin(ctx context.Context, svcCtx *svc.ServiceContext, logger *zap.SugaredLogger, userInfo *user.User, plainPassword string) {
	if userInfo == nil || userInfo.Id == 0 || plainPassword == "" {
		return
	}
	if !tool.PasswordNeedsRehash(userInfo.Algo, userInfo.Password) {
		return
	}

	nextHash := tool.EncodePassWord(plainPassword)
	updated, err := svcCtx.Store.User().UpgradePasswordHash(ctx, userInfo.Id, userInfo.Password, nextHash, tool.PasswordAlgoArgon2id, "")
	if err != nil {
		logger.Errorw("failed to upgrade password hash",
			zap.Int64("user_id", userInfo.Id),
			zap.String("error", err.Error()),
		)
		return
	}
	if !updated {
		return
	}
	userInfo.Password = nextHash
	userInfo.Algo = tool.PasswordAlgoArgon2id
	userInfo.Salt = ""
}
