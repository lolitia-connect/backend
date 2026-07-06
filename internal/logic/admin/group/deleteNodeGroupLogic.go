package group

import (
	"context"
	"errors"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	entnodegroup "github.com/perfect-panel/server/ent/nodegroup"
	"github.com/perfect-panel/server/ent/predicate"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
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
	nodeGroup, err := l.svcCtx.Ent.NodeGroup.Query().Where(entnodegroup.ID(req.Id)).Only(l.ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New("node group not found")
		}
		logger.Errorf("failed to find node group: %v", err)
		return err
	}

	// 检查是否有关联节点（使用JSON_CONTAINS查询node_group_ids数组）
	nodeCount, err := l.svcCtx.Ent.Node.Query().Where(predicate.Node(func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("JSON_CONTAINS(").Ident(entnode.FieldNodeGroupIds).WriteString(", ").Arg(fmt.Sprintf("[%d]", nodeGroup.ID)).WriteByte(')')
		}))
	})).Count(l.ctx)
	if err != nil {
		logger.Errorf("failed to count nodes in group: %v", err)
		return err
	}
	if nodeCount > 0 {
		return fmt.Errorf("cannot delete group with %d associated nodes, please migrate nodes first", nodeCount)
	}

	if _, err := l.svcCtx.Ent.NodeGroup.Delete().Where(entnodegroup.ID(req.Id)).Exec(l.ctx); err != nil {
		logger.Errorf("failed to delete node group: %v", err)
		return err
	}

	logger.Infof("deleted node group: id=%d", nodeGroup.ID)
	return nil
}
