package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UpdateBindMobileLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Bind Mobile
func NewUpdateBindMobileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBindMobileLogic {
	return &UpdateBindMobileLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateBindMobileLogic) UpdateBindMobile(req *types.UpdateBindMobileRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// verify mobile
	phoneNumber, err := phone.FormatToE164(req.AreaCode, req.Mobile)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.Register, phoneNumber)
	if err := usersession.ConsumeCode(l.ctx, l.svcCtx.Redis, cacheKey, req.Code); err != nil {
		return err
	}
	m, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if m != nil && m.UserId != u.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "mobile already bind")
	}

	method, err := l.svcCtx.Store.User().FindUserAuthMethodByUserId(l.ctx, "mobile", u.Id)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if method != nil && method.AuthIdentifier != phoneNumber {
		if err := verifyCurrentIdentity(l.ctx, l.svcCtx, u, req.Password, req.CurrentCode, false); err != nil {
			return err
		}
	}
	if method == nil {
		method = &user.AuthMethods{
			UserId:         u.Id,
			AuthType:       "mobile",
			AuthIdentifier: phoneNumber,
			Verified:       true,
		}
		if err := l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertUserAuthMethods error")
		}
	} else {
		method.Verified = true
		method.AuthIdentifier = phoneNumber
		if err := l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
		}
	}
	_, err = usersession.Rotate(l.ctx, l.svcCtx.Redis, u.Id)
	return err
}
