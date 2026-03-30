package group

import (
	"context"
	"errors"
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
	nodeGroupType, err := group.ResolveNodeGroupType(req.Type)
	if err != nil {
		return err
	}

	// 验证:系统中只能有一个过期节点组
	if req.IsExpiredGroup != nil && *req.IsExpiredGroup {
		var count int64
		err = l.svcCtx.DB.Model(&group.NodeGroup{}).
			Where("is_expired_group = ?", true).
			Count(&count).Error
		if err != nil {
			logger.Errorf("failed to check expired group count: %v", err)
			return err
		}
		if count > 0 {
			return errors.New("system already has an expired node group, cannot create multiple")
		}
	}

	// 创建节点组
	nodeGroup := &group.NodeGroup{
		Name:                req.Name,
		Type:                nodeGroupType,
		Description:         req.Description,
		Sort:                req.Sort,
		ForCalculation:      req.ForCalculation,
		IsExpiredGroup:      req.IsExpiredGroup,
		MaxTrafficGBExpired: req.MaxTrafficGBExpired,
		MinTrafficGB:        req.MinTrafficGB,
		MaxTrafficGB:        req.MaxTrafficGB,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	// 设置过期节点组的默认值
	if req.IsExpiredGroup != nil && *req.IsExpiredGroup {
		// 过期节点组不参与分组计算
		falseValue := false
		nodeGroup.ForCalculation = &falseValue

		if req.ExpiredDaysLimit != nil {
			nodeGroup.ExpiredDaysLimit = *req.ExpiredDaysLimit
		} else {
			nodeGroup.ExpiredDaysLimit = 7 // 默认7天
		}
		if req.SpeedLimit != nil {
			nodeGroup.SpeedLimit = *req.SpeedLimit
		}
	}

	if err := l.svcCtx.DB.Create(nodeGroup).Error; err != nil {
		logger.Errorf("failed to create node group: %v", err)
		return err
	}

	logger.Infof("created node group: node_group_id=%d", nodeGroup.Id)
	return nil
}
