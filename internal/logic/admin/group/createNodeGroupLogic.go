package group

import (
	"context"
	"errors"
	"time"

	entnodegroup "github.com/perfect-panel/server/ent/nodegroup"
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
		count, err := l.svcCtx.Ent.NodeGroup.Query().Where(entnodegroup.IsExpiredGroup(true)).Count(l.ctx)
		if err != nil {
			logger.Errorf("failed to check expired group count: %v", err)
			return err
		}
		if count > 0 {
			return errors.New("system already has an expired node group, cannot create multiple")
		}
	}

	now := time.Now()
	create := l.svcCtx.Ent.NodeGroup.Create().
		SetName(req.Name).
		SetType(nodeGroupType).
		SetDescription(req.Description).
		SetSort(req.Sort).
		SetNillableForCalculation(req.ForCalculation).
		SetNillableIsExpiredGroup(req.IsExpiredGroup).
		SetNillableMaxTrafficGBExpired(req.MaxTrafficGBExpired).
		SetNillableMinTrafficGB(req.MinTrafficGB).
		SetNillableMaxTrafficGB(req.MaxTrafficGB).
		SetCreatedAt(now).
		SetUpdatedAt(now)

	// 设置过期节点组的默认值
	if req.IsExpiredGroup != nil && *req.IsExpiredGroup {
		// 过期节点组不参与分组计算
		create.SetForCalculation(false)

		if req.ExpiredDaysLimit != nil {
			create.SetExpiredDaysLimit(*req.ExpiredDaysLimit)
		} else {
			create.SetExpiredDaysLimit(7) // 默认7天
		}
		if req.SpeedLimit != nil {
			create.SetSpeedLimit(*req.SpeedLimit)
		}
	}

	nodeGroup, err := create.Save(l.ctx)
	if err != nil {
		logger.Errorf("failed to create node group: %v", err)
		return err
	}

	logger.Infof("created node group: node_group_id=%d", nodeGroup.ID)
	return nil
}
