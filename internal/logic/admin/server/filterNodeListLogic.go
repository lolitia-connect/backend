package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type FilterNodeListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterNodeListLogic Filter Node List
func NewFilterNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterNodeListLogic {
	return &FilterNodeListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterNodeListLogic) FilterNodeList(req *types.FilterNodeListRequest) (resp *types.FilterNodeListResponse, err error) {
	params := &node.FilterNodeParams{
		Page:   req.Page,
		Size:   req.Size,
		Search: req.Search,
	}
	if req.NodeGroupId != nil {
		params.NodeGroupIds = []int64{*req.NodeGroupId}
	}
	total, data, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, params)

	if err != nil {
		l.Logger.Errorw("[FilterNodeList] Query Database Error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterNodeList] Query Database Error")
	}

	list := make([]types.Node, 0)
	for _, datum := range data {
		list = append(list, types.Node{
			Id:           datum.Id,
			Name:         datum.Name,
			Tags:         tool.RemoveDuplicateElements(strings.Split(datum.Tags, ",")...),
			Port:         datum.Port,
			Address:      datum.Address,
			ServerId:     datum.ServerId,
			Protocol:     datum.Protocol,
			ProtocolId:   datum.ProtocolId,
			Enabled:      datum.Enabled,
			NodeType:     datum.NodeType,
			IsHidden:     datum.IsHidden,
			Sort:         datum.Sort,
			NodeGroupIds: tool.Int64SliceToStringSlice([]int64(datum.NodeGroupIds)),
			CreatedAt:    datum.CreatedAt.UnixMilli(),
			UpdatedAt:    datum.UpdatedAt.UnixMilli(),
		})
	}

	return &types.FilterNodeListResponse{
		List:  list,
		Total: total,
	}, nil
}
