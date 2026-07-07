package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetServerProtocolsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Server Protocols
func NewGetServerProtocolsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetServerProtocolsLogic {
	return &GetServerProtocolsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerProtocolsLogic) GetServerProtocols(req *types.GetServerProtocolsRequest) (resp *types.GetServerProtocolsResponse, err error) {
	// find server
	data, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("[GetServerProtocols] FindOneServer Error: %s", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[GetServerProtocols] FindOneServer Error: %s", err.Error())
	}

	// handler protocols
	var protocols []types.Protocol
	dst, err := data.UnmarshalProtocols()
	if err != nil {
		l.Logger.Errorf("[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
	}
	tool.DeepCopy(&protocols, dst)

	return &types.GetServerProtocolsResponse{
		Protocols: protocols,
	}, nil
}
