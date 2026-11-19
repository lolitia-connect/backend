package oauth

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
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

// NewOAuthRegisterLogic completes an invitation-gated OAuth registration that
// was deferred by /v1/auth/oauth/login/token.
func NewOAuthRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthRegisterLogic {
	return &OAuthRegisterLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// OAuthRegister finishes registering a user whose provider identity was
// already verified and cached under a one-time ticket. It requires a valid
// invitation code and only then writes the user record and issues a session.
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

	if req.Invite == "" {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
	}

	// Reuse the shared find-or-register path so an identity that was completed
	// in another tab resolves to the existing account rather than failing on a
	// duplicate. The invitation code is validated inside register.
	tokenLogic := NewOAuthLoginGetTokenLogic(l.ctx, l.svcCtx)
	userInfo, err := tokenLogic.findOrRegisterUser(identity.Method, identity.OpenID, identity.Email, identity.Avatar, req.Invite, requestID, ip, userAgent)
	if err != nil {
		if _, ok := err.(*inviteRequiredError); ok {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
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
