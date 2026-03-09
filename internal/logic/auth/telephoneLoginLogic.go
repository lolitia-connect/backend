package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/captcha"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type TelephoneLoginLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// User Telephone login
func NewTelephoneLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelephoneLoginLogic {
	return &TelephoneLoginLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelephoneLoginLogic) TelephoneLogin(req *types.TelephoneLoginRequest, r *http.Request, ip string) (resp *types.LoginResponse, err error) {
	phoneNumber, err := phone.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}
	if !l.svcCtx.Config.Mobile.Enable {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SmsNotEnabled), "sms login is not enabled")
	}
	loginStatus := false

	authMethodInfo, err := l.svcCtx.UserModel.FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil {
		if errors.As(err, gorm.ErrRecordNotFound) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user telephone not exist: %v", req.Telephone)
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}

	userInfo, err := l.svcCtx.UserModel.FindOne(l.ctx, authMethodInfo.UserId)
	if err != nil {
		if errors.As(err, gorm.ErrRecordNotFound) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user telephone not exist: %v", req.Telephone)
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}
	// Record login status
	defer func(svcCtx *svc.ServiceContext) {
		if userInfo.Id != 0 {
			loginLog := log.Login{
				Method:    "mobile",
				LoginIP:   ip,
				UserAgent: r.UserAgent(),
				Success:   loginStatus,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := loginLog.Marshal()
			if err := l.svcCtx.LogModel.Insert(l.ctx, &log.SystemLog{
				Id:       0,
				Type:     log.TypeLogin.Uint8(),
				Date:     time.Now().Format("2006-01-02"),
				ObjectID: userInfo.Id,
				Content:  string(content),
			}); err != nil {
				l.Errorw("failed to insert login log",
					logger.Field("user_id", userInfo.Id),
					logger.Field("ip", req.IP),
					logger.Field("error", err.Error()),
				)
			}
		}
	}(l.svcCtx)

	if req.Password == "" && req.TelephoneCode == "" {
		return nil, xerr.NewErrCodeMsg(xerr.InvalidParams, "password and telephone code is empty")
	}

	// Verify captcha
	if err := l.verifyCaptcha(req); err != nil {
		return nil, err
	}

	if req.TelephoneCode == "" {
		// Verify password
		if !tool.MultiPasswordVerify(userInfo.Algo, userInfo.Salt, req.Password, userInfo.Password) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserPasswordError), "user password")
		}
	} else {
		cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.ParseVerifyType(uint8(constant.Security)), phoneNumber)
		value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
		if err != nil {
			l.Errorw("Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}

		if value == "" {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}

		var payload common.CacheKeyPayload
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			l.Errorw("[SendSmsCode]: Unmarshal Error", logger.Field("error", err.Error()), logger.Field("value", value))
		}

		if payload.Code != req.TelephoneCode {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}
		l.svcCtx.Redis.Del(l.ctx, cacheKey)
	}

	// Bind device to user if identifier is provided
	if req.Identifier != "" {
		bindLogic := NewBindDeviceLogic(l.ctx, l.svcCtx)
		if err := bindLogic.BindDeviceToUser(req.Identifier, req.IP, req.UserAgent, userInfo.Id); err != nil {
			l.Errorw("failed to bind device to user",
				logger.Field("user_id", userInfo.Id),
				logger.Field("identifier", req.Identifier),
				logger.Field("error", err.Error()),
			)
			// Don't fail login if device binding fails, just log the error
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
		l.Logger.Error("[UserLogin] token generate error", logger.Field("error", err.Error()))
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

func (l *TelephoneLoginLogic) verifyCaptcha(req *types.TelephoneLoginRequest) error {
	// Get verify config from database
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[TelephoneLoginLogic] GetVerifyConfig error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	var config struct {
		CaptchaType            string `json:"captcha_type"`
		EnableUserLoginCaptcha bool   `json:"enable_user_login_captcha"`
		TurnstileSecret        string `json:"turnstile_secret"`
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &config)

	// Check if captcha is enabled for user login
	if !config.EnableUserLoginCaptcha {
		return nil
	}

	// Verify based on captcha type
	if config.CaptchaType == "local" {
		if req.CaptchaId == "" || req.CaptchaCode == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "captcha required")
		}

		captchaService := captcha.NewService(captcha.Config{
			Type:        captcha.CaptchaTypeLocal,
			RedisClient: l.svcCtx.Redis,
		})

		valid, err := captchaService.Verify(l.ctx, req.CaptchaId, req.CaptchaCode, req.IP)
		if err != nil {
			l.Logger.Error("[TelephoneLoginLogic] Verify captcha error: ", logger.Field("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify captcha error")
		}

		if !valid {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "invalid captcha")
		}
	} else if config.CaptchaType == "turnstile" {
		if req.CfToken == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "captcha required")
		}

		captchaService := captcha.NewService(captcha.Config{
			Type:            captcha.CaptchaTypeTurnstile,
			TurnstileSecret: config.TurnstileSecret,
		})

		valid, err := captchaService.Verify(l.ctx, req.CfToken, "", req.IP)
		if err != nil {
			l.Logger.Error("[TelephoneLoginLogic] Verify turnstile error: ", logger.Field("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify captcha error")
		}

		if !valid {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "invalid captcha")
		}
	}

	return nil
}
