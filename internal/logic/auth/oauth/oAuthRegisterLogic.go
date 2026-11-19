package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type OAuthRegisterLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewOAuthRegisterLogic completes an OAuth registration that was deferred by
// /v1/auth/oauth/login/token because the panel still needed input from the
// user (an invitation code and/or a real email address).
func NewOAuthRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthRegisterLogic {
	return &OAuthRegisterLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// OAuthRegister finishes registering a user whose provider identity was already
// verified and cached under a one-time ticket. It requires whatever the panel
// could not obtain from the provider: an invitation code when registration is
// invitation-only, and a real, user-supplied email address when the provider
// returned none (e.g. Telegram). A collected email is only persisted as
// verified once its verification code has been checked.
func (l *OAuthRegisterLogic) OAuthRegister(req *types.OAuthRegisterRequest, ip, userAgent string) (*types.LoginResponse, error) {
	requestID := uuidx.NewUUID().String()

	identity, err := loadPendingOAuth(l.ctx, l.svcCtx.Redis, req.OAuthToken)
	if err != nil || identity == nil {
		l.Logger.Errorw("pending oauth registration not found or expired",
			zap.Any("request_id", requestID),
			zap.Any("error", err),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "oauth registration session expired")
	}

	if req.Invite == "" && l.svcCtx.Config.Invite.ForcedInvite {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
	}

	// Resolve the email to register with. A provider-supplied address is
	// trusted as-is; when the provider returned none the user must supply a
	// real mailbox, which is only trusted once its code has been verified.
	email := identity.Email
	emailVerified := true
	if email == "" {
		email, emailVerified, err = l.resolveCollectedEmail(req, requestID)
		if err != nil {
			return nil, err
		}
	}

	// Reuse the shared find-or-register path so an identity that was completed
	// in another tab resolves to the existing account rather than failing on a
	// duplicate. The invitation code is validated inside register.
	tokenLogic := NewOAuthLoginGetTokenLogic(l.ctx, l.svcCtx)
	userInfo, err := tokenLogic.findOrRegisterUser(identity.Method, identity.OpenID, email, identity.Avatar, req.Invite, emailVerified, requestID, ip, userAgent)
	if err != nil {
		if pending, ok := err.(*oauthRegistrationPending); ok {
			if pending.inviteRequired {
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
			}
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "email is required")
		}
		return nil, err
	}

	token, err := tokenLogic.generateToken(userInfo, requestID)
	if err != nil {
		return nil, err
	}

	// Consume the one-time ticket only after the account exists and a session
	// has been issued, so a rejected code can be retried with another.
	consumePendingOAuth(l.ctx, l.svcCtx.Redis, req.OAuthToken)

	tokenLogic.recordLoginStatus(true, userInfo, ip, userAgent, requestID, identity.Method)

	return &types.LoginResponse{Token: token}, nil
}

// resolveCollectedEmail validates the email the user supplied to finish an
// OAuth registration whose provider returned no address. It returns the
// normalised address and whether it should be stored as verified. When email
// verification is enabled the supplied code must match the one issued through
// /v1/common/send_code for the register flow.
func (l *OAuthRegisterLogic) resolveCollectedEmail(req *types.OAuthRegisterRequest, requestID string) (string, bool, error) {
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return "", false, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "email is required")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", false, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid email address")
	}

	if !l.svcCtx.Config.Email.EnableVerify {
		// The panel does not verify emails, mirroring the ordinary email
		// registration path which stores such addresses as unverified.
		return email, false, nil
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Register, email)
	value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if err != nil {
		l.Logger.Errorw("oauth email verification code not found",
			zap.Any("request_id", requestID),
			zap.Any("cacheKey", cacheKey),
			zap.Any("error", err.Error()),
		)
		return "", false, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	var payload common.CacheKeyPayload
	if err = json.Unmarshal([]byte(value), &payload); err != nil {
		l.Logger.Errorw("oauth email verification code corrupt",
			zap.Any("request_id", requestID),
			zap.Any("error", err.Error()),
		)
		return "", false, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	if payload.Code == "" || payload.Code != req.Code {
		return "", false, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	return email, true, nil
}
