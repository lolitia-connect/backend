package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/nodeconfig"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateServerNodeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateServerNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateServerNodeConfigLogic {
	return &UpdateServerNodeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateServerNodeConfigLogic) UpdateServerNodeConfig(req *types.UpdateServerNodeConfigRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	if _, err := nodeStore.FindOneServer(l.ctx, req.ServerID); err != nil {
		l.Logger.Errorf("[UpdateServerNodeConfig] FindOneServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server error: %v", err)
	}

	data, allInherited, err := nodeconfig.OverrideModel(req.ServerID, req.ServerNodeConfigOverride)
	if err != nil {
		l.Logger.Errorf("[UpdateServerNodeConfig] OverrideModel Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "server node config is invalid"), "server node config is invalid: %v", err)
	}

	if allInherited {
		err = nodeStore.DeleteServerConfigOverride(l.ctx, req.ServerID)
	} else {
		err = nodeStore.SaveServerConfigOverride(l.ctx, data)
	}
	if err != nil {
		l.Logger.Errorf("[UpdateServerNodeConfig] SaveServerConfigOverride Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server node config error: %v", err)
	}

	return nodeStore.ClearServerCache(l.ctx, req.ServerID)
}
