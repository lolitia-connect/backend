package group

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type CreateNodeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateNodeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNodeGroupLogic {
	return &CreateNodeGroupLogic{
		Logger: zap.S(),
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
		count, err := l.svcCtx.Store.Group().CountExpiredNodeGroups(l.ctx)
		if err != nil {
			zap.S().Errorf("failed to check expired group count: %v", err)
			return err
		}
		if count > 0 {
			return errors.New("system already has an expired node group, cannot create multiple")
		}
	}

	now := time.Now()
	data := &group.NodeGroup{Name: req.Name, Type: nodeGroupType, Description: req.Description, Sort: req.Sort, ForCalculation: req.ForCalculation, IsExpiredGroup: req.IsExpiredGroup, MaxTrafficGBExpired: req.MaxTrafficGBExpired, MinTrafficGB: req.MinTrafficGB, MaxTrafficGB: req.MaxTrafficGB, CreatedAt: now, UpdatedAt: now}

	// 设置过期节点组的默认值
	if req.IsExpiredGroup != nil && *req.IsExpiredGroup {
		// 过期节点组不参与分组计算
		forCalculation := false
		data.ForCalculation = &forCalculation

		if req.ExpiredDaysLimit != nil {
			data.ExpiredDaysLimit = *req.ExpiredDaysLimit
		} else {
			data.ExpiredDaysLimit = 7 // 默认7天
		}
		if req.SpeedLimit != nil {
			data.SpeedLimit = *req.SpeedLimit
		}
	}

	nodeGroup, err := l.svcCtx.Store.Group().CreateNodeGroup(l.ctx, data)
	if err != nil {
		zap.S().Errorf("failed to create node group: %v", err)
		return err
	}

	zap.S().Infof("created node group: node_group_id=%d", nodeGroup.Id)
	return nil
}
