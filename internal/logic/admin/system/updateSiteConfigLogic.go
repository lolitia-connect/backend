package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateSiteConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSiteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSiteConfigLogic {
	return &UpdateSiteConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSiteConfigLogic) UpdateSiteConfig(req *types.SiteConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "site", stringConfigFields(*req), config.SiteConfigKey, config.GlobalConfigKey)
	if err != nil {
		l.Logger.Error("[UpdateSiteConfig] update site config error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update site config error: %v", err.Error())
	}
	initialize.Site(l.svcCtx)
	return nil
}
