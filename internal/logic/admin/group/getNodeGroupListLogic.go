package group

import (
	"context"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetNodeGroupListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodeGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeGroupListLogic {
	return &GetNodeGroupListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeGroupListLogic) GetNodeGroupList(req *types.GetNodeGroupListRequest) (resp *types.GetNodeGroupListResponse, err error) {
	var nodeGroups []group.NodeGroup
	var total int64

	// 构建查询
	query := l.svcCtx.DB.Model(&group.NodeGroup{})

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		logger.Errorf("failed to count node groups: %v", err)
		return nil, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.Size
	if err := query.Order("sort ASC").Offset(offset).Limit(req.Size).Find(&nodeGroups).Error; err != nil {
		logger.Errorf("failed to find node groups: %v", err)
		return nil, err
	}

	// 转换为响应格式
	var list []types.NodeGroup
	for _, ng := range nodeGroups {
		// 统计该组的节点数
		var nodeCount int64
		l.svcCtx.DB.Table("nodes").Where("node_group_id = ?", ng.Id).Count(&nodeCount)

		// 处理指针类型的字段
		var forCalculation bool
		if ng.ForCalculation != nil {
			forCalculation = *ng.ForCalculation
		} else {
			forCalculation = true // 默认值
		}

		var minTrafficGB, maxTrafficGB int64
		if ng.MinTrafficGB != nil {
			minTrafficGB = *ng.MinTrafficGB
		}
		if ng.MaxTrafficGB != nil {
			maxTrafficGB = *ng.MaxTrafficGB
		}

		list = append(list, types.NodeGroup{
			Id:             ng.Id,
			Name:           ng.Name,
			Description:    ng.Description,
			Sort:           ng.Sort,
			ForCalculation: forCalculation,
			MinTrafficGB:   minTrafficGB,
			MaxTrafficGB:   maxTrafficGB,
			NodeCount:      nodeCount,
			CreatedAt:      ng.CreatedAt.Unix(),
			UpdatedAt:      ng.UpdatedAt.Unix(),
		})
	}

	resp = &types.GetNodeGroupListResponse{
		Total: total,
		List:  list,
	}

	return resp, nil
}
