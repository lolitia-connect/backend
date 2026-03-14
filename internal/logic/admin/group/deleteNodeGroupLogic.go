package group

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

type DeleteNodeGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteNodeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteNodeGroupLogic {
	return &DeleteNodeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteNodeGroupLogic) DeleteNodeGroup(req *types.DeleteNodeGroupRequest) error {
	// 查询节点组信息
	var nodeGroup group.NodeGroup
	if err := l.svcCtx.DB.Where("id = ?", req.Id).First(&nodeGroup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("node group not found")
		}
		logger.Errorf("failed to find node group: %v", err)
		return err
	}

	// 检查是否有关联节点（使用JSON_CONTAINS查询node_group_ids数组）
	var nodeCount int64
	if err := l.svcCtx.DB.Model(&node.Node{}).Where("JSON_CONTAINS(node_group_ids, ?)", fmt.Sprintf("[%d]", nodeGroup.Id)).Count(&nodeCount).Error; err != nil {
		logger.Errorf("failed to count nodes in group: %v", err)
		return err
	}
	if nodeCount > 0 {
		return fmt.Errorf("cannot delete group with %d associated nodes, please migrate nodes first", nodeCount)
	}

	// 使用 GORM Transaction 删除节点组
	return l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 删除节点组
		if err := tx.Where("id = ?", req.Id).Delete(&group.NodeGroup{}).Error; err != nil {
			logger.Errorf("failed to delete node group: %v", err)
			return err // 自动回滚
		}

		logger.Infof("deleted node group: id=%d", nodeGroup.Id)
		return nil // 自动提交
	})
}
