package authMethod

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/sms"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateAuthMethodConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update auth method config
func NewUpdateAuthMethodConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAuthMethodConfigLogic {
	return &UpdateAuthMethodConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAuthMethodConfigLogic) UpdateAuthMethodConfig(req *types.UpdateAuthMethodConfigRequest) (resp *types.AuthMethodConfig, err error) {
	method, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, req.Method)
	if err != nil {
		l.Logger.Errorw("find one by method failed", zap.Any("method", req.Method), zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find one by method failed: %v", err.Error())
	}

	if req.Enabled != nil {
		method.Enabled = req.Enabled
	}
	if req.Config != nil {
		if _, ok := req.Config.(map[string]interface{}); !ok {
			return nil, errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "config must be an object")
		}
		config := req.Config
		configs, err := json.Marshal(config)
		if err != nil {
			return nil, errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "invalid config")
		}
		if req.Method == "email" {
			emailConfig := new(auth.EmailAuthConfig)
			if err := json.Unmarshal(configs, emailConfig); err != nil {
				return nil, errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "invalid email config")
			}
			config = emailConfig
		}
		if req.Method == "mobile" {
			mobileConfig := new(auth.MobileAuthConfig)
			if err := json.Unmarshal(configs, mobileConfig); err != nil {
				return nil, errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "invalid mobile config")
			}
			config = mobileConfig
		}
		bytes, err := json.Marshal(config)
		if err != nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "marshal config failed: %v", err.Error())
		}
		method.Config = string(bytes)
	} else if method.Config == "" {
		config, ok := initializePlatformConfig(req.Method).(string)
		if !ok {
			return nil, errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "unsupported auth method")
		}
		method.Config = config
	}
	err = l.svcCtx.Store.Auth().Update(l.ctx, method)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "update auth method failed: %v", err.Error())
	}

	resp = new(types.AuthMethodConfig)
	tool.DeepCopy(resp, method)
	if method.Config != "" {
		if err := json.Unmarshal([]byte(method.Config), &resp.Config); err != nil {
			l.Logger.Errorw("unmarshal apple config failed", zap.Any("config", method.Config), zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
		}
	}
	// update global config
	defer l.UpdateGlobal(method.Method)
	return
}

func (l *UpdateAuthMethodConfigLogic) UpdateGlobal(method string) {
	if method == "email" {
		initialize.Email(l.svcCtx)
	}
	if method == "mobile" {
		initialize.Mobile(l.svcCtx)
	}
	if method == "device" {
		initialize.Device(l.svcCtx)
	}
}

func validatePlatformConfig(platform string, cfg map[string]interface{}) (interface{}, error) {
	var bytes []byte
	var err error
	var config interface{}
	bytes, err = json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	switch platform {
	case email.SMTP.String():
		config = new(auth.SMTPConfig)
	case sms.Abosend.String():
		config = new(auth.AbosendConfig)
	case sms.AlibabaCloud.String():
		config = new(auth.AlibabaCloudConfig)
	case sms.Smsbao.String():
		config = new(auth.SmsbaoConfig)
	case sms.Twilio.String():
		config = new(auth.TwilioConfig)
	default:
		return nil, errors.New("invalid platform")
	}
	err = json.Unmarshal(bytes, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func initializePlatformConfig(platform string) interface{} {
	var result interface{}
	switch platform {
	case "email":
		result = new(auth.EmailAuthConfig).Marshal()
	case "mobile":
		result = new(auth.MobileAuthConfig).Marshal()
	case "apple":
		result = new(auth.AppleAuthConfig).Marshal()
	case "google":
		result = new(auth.GoogleAuthConfig).Marshal()
	case "github":
		result = new(auth.GithubAuthConfig).Marshal()
	case "facebook":
		result = new(auth.FacebookAuthConfig).Marshal()
	case "telegram":
		result = new(auth.TelegramAuthConfig).Marshal()
	case "device":
		result = new(auth.DeviceConfig).Marshal()
	}
	return result
}
