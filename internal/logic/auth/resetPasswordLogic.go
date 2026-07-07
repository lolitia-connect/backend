package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/perfect-panel/server/ent"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/captcha"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/uuidx"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
)

type ResetPasswordLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetPasswordLogic Reset password
func NewResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetPasswordLogic {
	return &ResetPasswordLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetPasswordLogic) ResetPassword(req *types.ResetPasswordRequest) (resp *types.LoginResponse, err error) {
	var userInfo *user.User
	loginStatus := false

	defer func() {
		if userInfo != nil && userInfo.Id != 0 && loginStatus {
			loginLog := log.Login{
				Method:    "email",
				LoginIP:   req.IP,
				UserAgent: req.UserAgent,
				Success:   loginStatus,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := loginLog.Marshal()
			if err := l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
				Id:       0,
				Type:     log.TypeLogin.Uint8(),
				Date:     time.Now().Format("2006-01-02"),
				ObjectID: userInfo.Id,
				Content:  string(content),
			}); err != nil {
				l.Logger.Errorw("failed to insert login log",
					zap.Any("user_id", userInfo.Id),
					zap.Any("ip", req.IP),
					zap.Any("error", err.Error()),
				)
			}
		}
	}()

	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, req.Email)
	// Check the verification code
	if value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result(); err != nil {
		l.Logger.Errorw("Verification code error", zap.Any("cacheKey", cacheKey), zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "Verification code error")
	} else {
		var payload CacheKeyPayload
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			l.Logger.Errorw("Unmarshal errors", zap.Any("cacheKey", cacheKey), zap.Any("error", err.Error()), zap.Any("value", value))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "Verification code error")
		}
		if payload.Code != req.Code {
			l.Logger.Errorw("Verification code error", zap.Any("cacheKey", cacheKey), zap.Any("error", "Verification code error"), zap.Any("reqCode", req.Code), zap.Any("payloadCode", payload.Code))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "Verification code error")
		}
	}

	// Verify captcha
	if err := l.verifyCaptcha(req); err != nil {
		return nil, err
	}

	// Check user
	authMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "email", req.Email)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user email not exist: %v", req.Email)
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user by email error: %v", err.Error())
	}

	userInfo, err = l.svcCtx.Store.User().FindOne(l.ctx, authMethod.UserId)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user email not exist: %v", req.Email)
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}

	// Update password
	userInfo.Password = tool.EncodePassWord(req.Password)
	userInfo.Algo = "default"
	if err = l.svcCtx.Store.User().Update(l.ctx, userInfo); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update user info failed: %v", err.Error())
	}

	// Bind device to user if identifier is provided
	if req.Identifier != "" {
		bindLogic := NewBindDeviceLogic(l.ctx, l.svcCtx)
		if err := bindLogic.BindDeviceToUser(req.Identifier, req.IP, req.UserAgent, userInfo.Id); err != nil {
			l.Logger.Errorw("failed to bind device to user",
				zap.Any("user_id", userInfo.Id),
				zap.Any("identifier", req.Identifier),
				zap.Any("error", err.Error()),
			)
			// Don't fail register if device binding fails, just log the error
		}
	}
	if l.ctx.Value(constant.CtxLoginType) != nil {
		req.LoginType = l.ctx.Value(constant.CtxLoginType).(string)
	}
	// Generate session id
	sessionId := uuidx.NewUUID().String()
	// Generate token
	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		time.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userInfo.Id),
		jwt.WithOption("SessionId", sessionId),
		jwt.WithOption("identifier", req.Identifier),
		jwt.WithOption("CtxLoginType", req.LoginType),
	)
	if err != nil {
		l.Logger.Error("[UserLogin] token generate error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err.Error())
	}
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err = l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err.Error())
	}
	loginStatus = true
	return &types.LoginResponse{
		Token: token,
	}, nil
}

func (l *ResetPasswordLogic) verifyCaptcha(req *types.ResetPasswordRequest) error {
	verifyCfg, err := l.svcCtx.Store.System().GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[ResetPasswordLogic] GetVerifyConfig error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	var cfg struct {
		CaptchaType                    string `json:"captcha_type"`
		EnableUserResetPasswordCaptcha bool   `json:"enable_user_reset_password_captcha"`
		TurnstileSecret                string `json:"turnstile_secret"`
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &cfg)

	if !cfg.EnableUserResetPasswordCaptcha {
		return nil
	}

	return captcha.VerifyCaptcha(l.ctx, l.svcCtx.Redis, cfg.CaptchaType, cfg.TurnstileSecret, captcha.VerifyInput{
		CaptchaId:   req.CaptchaId,
		CaptchaCode: req.CaptchaCode,
		CfToken:     req.CfToken,
		SliderToken: req.SliderToken,
		IP:          req.IP,
	})
}
