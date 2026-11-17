package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UpdateUserPasswordLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update User Password
func NewUpdateUserPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserPasswordLogic {
	return &UpdateUserPasswordLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserPasswordLogic) UpdateUserPassword(req *types.UpdateUserPasswordRequest) error {
	current, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || current == nil {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, current.Id)
	if err != nil {
		return err
	}
	if err := verifyCurrentIdentity(l.ctx, l.svcCtx, userInfo, req.OldPassword, req.CurrentCode, true); err != nil {
		return err
	}
	if err := l.svcCtx.Store.User().UpdatePassword(l.ctx, userInfo.Id, userInfo.Password, tool.EncodePassWord(req.Password), tool.PasswordAlgoArgon2id, ""); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update user password error")
	}
	_, err = usersession.Rotate(l.ctx, l.svcCtx.Redis, userInfo.Id)
	return err
}

func verifyCurrentIdentity(ctx context.Context, svcCtx *svc.ServiceContext, account *user.User, password, currentCode string, changingPassword bool) error {
	if account == nil || account.DeletedAt != nil || account.Enable == nil || !*account.Enable {
		return xerr.NewErrCode(xerr.UserDisabled)
	}
	if password != "" || (changingPassword && account.Password != "") {
		return usersession.VerifyPassword(ctx, svcCtx.Redis, account.Id, account.Algo, account.Salt, password, account.Password)
	}
	for _, binding := range account.AuthMethods {
		prefix := config.AuthCodeCacheKey
		if binding.AuthType == "mobile" {
			prefix = config.AuthCodeTelephoneCacheKey
		} else if binding.AuthType != "email" {
			continue
		}
		if usersession.ConsumeCode(ctx, svcCtx.Redis, fmt.Sprintf("%s:%s:%s", prefix, constant.Security, binding.AuthIdentifier), currentCode) == nil {
			return nil
		}
	}
	return xerr.NewErrCode(xerr.VerifyCodeError)
}
