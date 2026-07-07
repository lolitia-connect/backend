package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateNodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateNodeLogic Create Node
func NewCreateNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNodeLogic {
	return &CreateNodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateNodeLogic) CreateNode(req *types.CreateNodeRequest) error {
	data := node.Node{
		Name:         req.Name,
		Tags:         tool.StringSliceToString(req.Tags),
		Enabled:      req.Enabled,
		Port:         req.Port,
		Address:      req.Address,
		ServerId:     req.ServerId,
		Protocol:     req.Protocol,
		ProtocolId:   req.ProtocolId,
		NodeType:     req.NodeType,
		IsHidden:     req.IsHidden,
		NodeGroupIds: node.JSONInt64Slice(tool.StringSliceToInt64Slice(req.NodeGroupIds)),
	}
	err := l.svcCtx.Store.Node().InsertNode(l.ctx, &data)
	if err != nil {
		l.Logger.Errorw("[CreateNode] Insert Database Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "[CreateNode] Insert Database Error")
	}

	return l.svcCtx.Store.Node().ClearServerCache(l.ctx, data.ServerId)
}
