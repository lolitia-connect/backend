package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UpdateBindEmailLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateBindEmailLogic Update Bind Email
func NewUpdateBindEmailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBindEmailLogic {
	return &UpdateBindEmailLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateBindEmailLogic) UpdateBindEmail(req *types.UpdateBindEmailRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	method, err := l.svcCtx.Store.User().FindUserAuthMethodByUserId(l.ctx, "email", u.Id)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	req.Email = authmethod.CanonicalEmail(req.Email)
	if method != nil && method.AuthIdentifier != req.Email {
		if err := verifyCurrentIdentity(l.ctx, l.svcCtx, u, req.Password, req.CurrentCode, false); err != nil {
			return err
		}
	}
	if err := usersession.ConsumeCode(l.ctx, l.svcCtx.Redis, fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Register, req.Email), req.Code); err != nil {
		return err
	}
	m, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "email", req.Email)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	// email already bind
	if m != nil && m.UserId != u.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "email already bind")
	}
	if method == nil || method.Id == 0 {
		method = &user.AuthMethods{
			UserId:         u.Id,
			AuthType:       "email",
			AuthIdentifier: req.Email,
			Verified:       true,
		}
		if err := l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertUserAuthMethods error")
		}
	} else {
		method.Verified = true
		method.AuthIdentifier = req.Email
		if err := l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
		}
	}
	_, err = usersession.Rotate(l.ctx, l.svcCtx.Redis, u.Id)
	return err
}
