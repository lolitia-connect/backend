package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/captcha"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type AdminLoginLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Admin login
func NewAdminLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminLoginLogic {
	return &AdminLoginLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminLoginLogic) AdminLogin(req *types.UserLoginRequest) (resp *types.LoginResponse, err error) {
	loginStatus := false
	var userInfo *user.User
	// Record login status
	defer func(svcCtx *svc.ServiceContext) {
		if userInfo != nil && userInfo.Id != 0 {
			loginLog := log.Login{
				Method:    "email",
				LoginIP:   req.IP,
				UserAgent: req.UserAgent,
				Success:   loginStatus,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := loginLog.Marshal()
			if err := l.svcCtx.LogModel.Insert(l.ctx, &log.SystemLog{
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

	// Verify captcha
	if err := l.verifyCaptcha(req); err != nil {
		return nil, err
	}

	userInfo, err = l.svcCtx.UserModel.FindOneByEmail(l.ctx, req.Email)

	if userInfo.DeletedAt.Valid {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user email deleted: %v", req.Email)
	}

	if err != nil {
		if errors.As(err, &gorm.ErrRecordNotFound) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user email not exist: %v", req.Email)
		}
		logger.WithContext(l.ctx).Error(err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	}

	// Check if user is admin
	if userInfo.IsAdmin == nil || !*userInfo.IsAdmin {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.PermissionDenied), "user is not admin")
	}

	// Verify password
	if !tool.MultiPasswordVerify(userInfo.Algo, userInfo.Salt, req.Password, userInfo.Password) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserPasswordError), "user password")
	}

	// Bind device to user if identifier is provided
	if req.Identifier != "" {
		bindLogic := auth.NewBindDeviceLogic(l.ctx, l.svcCtx)
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
		l.Logger.Error("[AdminLogin] token generate error", logger.Field("error", err.Error()))
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

func (l *AdminLoginLogic) verifyCaptcha(req *types.UserLoginRequest) error {
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[AdminLoginLogic] GetVerifyConfig error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	var cfg struct {
		CaptchaType             string `json:"captcha_type"`
		EnableAdminLoginCaptcha bool   `json:"enable_admin_login_captcha"`
		TurnstileSecret         string `json:"turnstile_secret"`
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &cfg)

	if !cfg.EnableAdminLoginCaptcha {
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
