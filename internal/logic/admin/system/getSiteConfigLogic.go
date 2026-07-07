package system

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetSiteConfigLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	Logger *zap.SugaredLogger
}

func NewGetSiteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSiteConfigLogic {
	return &GetSiteConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSiteConfigLogic) GetSiteConfig() (resp *types.SiteConfig, err error) {
	resp = &types.SiteConfig{}
	// get site config from db
	siteConfigs, err := l.svcCtx.Store.System().GetSiteConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetSiteConfig] Database query error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get site config failed: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(siteConfigs, resp)
	return resp, nil
}
