package group

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type CreateNodeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateNodeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNodeGroupLogic {
	return &CreateNodeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateNodeGroupLogic) CreateNodeGroup(req *types.CreateNodeGroupRequest) error {
	// 创建节点组
	nodeGroup := &group.NodeGroup{
		Name:           req.Name,
		Description:    req.Description,
		Sort:           req.Sort,
		ForCalculation: req.ForCalculation,
		MinTrafficGB:   req.MinTrafficGB,
		MaxTrafficGB:   req.MaxTrafficGB,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := l.svcCtx.DB.Create(nodeGroup).Error; err != nil {
		logger.Errorf("failed to create node group: %v", err)
		return err
	}

	logger.Infof("created node group: node_group_id=%d", nodeGroup.Id)
	return nil
}
