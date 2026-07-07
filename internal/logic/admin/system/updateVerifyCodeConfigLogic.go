package system

import (
	"context"

	"github.com/perfect-panel/server/internal/config"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UpdateVerifyCodeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Verify Code Config
func NewUpdateVerifyCodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateVerifyCodeConfigLogic {
	return &UpdateVerifyCodeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateVerifyCodeConfigLogic) UpdateVerifyCodeConfig(req *types.VerifyCodeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "verify_code", convertedConfigFields(*req), config.VerifyCodeConfigKey)
	if err != nil {
		l.Logger.Errorw("[UpdateRegisterConfig] update verify code config error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update register config error: %v", err.Error())
	}
	return nil
}
