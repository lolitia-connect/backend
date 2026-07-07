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

type UpdatePrivacyPolicyConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Privacy Policy Config
func NewUpdatePrivacyPolicyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdatePrivacyPolicyConfigLogic {
	return &UpdatePrivacyPolicyConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdatePrivacyPolicyConfigLogic) UpdatePrivacyPolicyConfig(req *types.PrivacyPolicyConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "tos", convertedConfigFields(*req), config.TosConfigKey)
	if err != nil {
		l.Logger.Errorw("[UpdateTosConfigLogic] update tos config error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update tos config error: %v", err)
	}

	return nil
}
