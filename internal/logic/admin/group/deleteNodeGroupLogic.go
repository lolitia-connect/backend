package group

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type DeleteNodeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteNodeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteNodeGroupLogic {
	return &DeleteNodeGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteNodeGroupLogic) DeleteNodeGroup(req *types.DeleteNodeGroupRequest) error {
	// 查询节点组信息
	nodeGroup, err := l.svcCtx.Store.Group().FindNodeGroup(l.ctx, req.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New("node group not found")
		}
		zap.S().Errorf("failed to find node group: %v", err)
		return err
	}

	// 检查是否有关联节点（使用JSON_CONTAINS查询node_group_ids数组）
	nodeCount, err := l.svcCtx.Store.Group().CountNodesByNodeGroupId(l.ctx, nodeGroup.Id)
	if err != nil {
		zap.S().Errorf("failed to count nodes in group: %v", err)
		return err
	}
	if nodeCount > 0 {
		return fmt.Errorf("cannot delete group with %d associated nodes, please migrate nodes first", nodeCount)
	}

	if err := l.svcCtx.Store.Group().DeleteNodeGroup(l.ctx, req.Id); err != nil {
		zap.S().Errorf("failed to delete node group: %v", err)
		return err
	}

	zap.S().Infof("deleted node group: id=%d", nodeGroup.Id)
	return nil
}
