package group

import (
	"context"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetNodeGroupListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodeGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeGroupListLogic {
	return &GetNodeGroupListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeGroupListLogic) GetNodeGroupList(req *types.GetNodeGroupListRequest) (resp *types.GetNodeGroupListResponse, err error) {
	total, nodeGroups, err := l.svcCtx.Store.Group().QueryNodeGroupList(l.ctx, req.Page, req.Size)
	if err != nil {
		zap.S().Errorf("failed to find node groups: %v", err)
		return nil, err
	}

	// 转换为响应格式
	var list []types.NodeGroup
	for _, ng := range nodeGroups {
		// 统计该组的节点数（JSON数组查询）
		nodeCount, _ := l.svcCtx.Store.Group().CountNodesByNodeGroupId(l.ctx, ng.Id)

		// 处理指针类型的字段
		var forCalculation bool
		if ng.ForCalculation != nil {
			forCalculation = *ng.ForCalculation
		}

		var isExpiredGroup bool
		if ng.IsExpiredGroup != nil {
			isExpiredGroup = *ng.IsExpiredGroup
		}

		var minTrafficGB, maxTrafficGB, maxTrafficGBExpired int64
		if ng.MinTrafficGB != nil {
			minTrafficGB = *ng.MinTrafficGB
		}
		if ng.MaxTrafficGB != nil {
			maxTrafficGB = *ng.MaxTrafficGB
		}
		if ng.MaxTrafficGBExpired != nil {
			maxTrafficGBExpired = *ng.MaxTrafficGBExpired
		}

		list = append(list, types.NodeGroup{
			Id:                  ng.Id,
			Name:                ng.Name,
			Type:                group.MustNodeGroupType(ng.Type),
			Description:         ng.Description,
			Sort:                ng.Sort,
			ForCalculation:      forCalculation,
			IsExpiredGroup:      isExpiredGroup,
			ExpiredDaysLimit:    ng.ExpiredDaysLimit,
			MaxTrafficGBExpired: maxTrafficGBExpired,
			SpeedLimit:          ng.SpeedLimit,
			MinTrafficGB:        minTrafficGB,
			MaxTrafficGB:        maxTrafficGB,
			NodeCount:           int64(nodeCount),
			CreatedAt:           ng.CreatedAt.Unix(),
			UpdatedAt:           ng.UpdatedAt.Unix(),
		})
	}

	resp = &types.GetNodeGroupListResponse{
		Total: total,
		List:  list,
	}

	return resp, nil
}
