package group

import (
	"context"

	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type ResetGroupsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetGroupsLogic Reset all groups (delete all node groups and reset related data)
func NewResetGroupsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetGroupsLogic {
	return &ResetGroupsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetGroupsLogic) ResetGroups() error {
	// 1. Delete all node groups
	_, err := l.svcCtx.Ent.NodeGroup.Delete().Exec(l.ctx)
	if err != nil {
		l.Errorw("Failed to delete all node groups", logger.Field("error", err.Error()))
		return err
	}
	l.Infow("Successfully deleted all node groups")

	// 2. Clear node_group_ids for all subscribes (products)
	_, err = l.svcCtx.Ent.Subscribe.Update().SetNodeGroupIds([]int64{}).Save(l.ctx)
	if err != nil {
		l.Errorw("Failed to clear subscribes' node_group_ids", logger.Field("error", err.Error()))
		return err
	}
	l.Infow("Successfully cleared all subscribes' node_group_ids")

	// 3. Clear node_group_ids for all nodes
	_, err = l.svcCtx.Ent.Node.Update().SetNodeGroupIds([]int64{}).Save(l.ctx)
	if err != nil {
		l.Errorw("Failed to clear nodes' node_group_ids", logger.Field("error", err.Error()))
		return err
	}
	l.Infow("Successfully cleared all nodes' node_group_ids")

	// 4. Clear group history
	_, err = l.svcCtx.Ent.GroupHistory.Delete().Exec(l.ctx)
	if err != nil {
		l.Errorw("Failed to clear group history", logger.Field("error", err.Error()))
		// Non-critical error, continue anyway
	} else {
		l.Infow("Successfully cleared group history")
	}

	// 7. Clear group history details
	_, err = l.svcCtx.Ent.GroupHistoryDetail.Delete().Exec(l.ctx)
	if err != nil {
		l.Errorw("Failed to clear group history details", logger.Field("error", err.Error()))
		// Non-critical error, continue anyway
	} else {
		l.Infow("Successfully cleared group history details")
	}

	// 5. Delete all group config settings
	_, err = l.svcCtx.Ent.System.Delete().Where(entsystem.Category("group")).Exec(l.ctx)
	if err != nil {
		l.Errorw("Failed to delete group config", logger.Field("error", err.Error()))
		return err
	}
	l.Infow("Successfully deleted all group config settings")

	l.Infow("Group reset completed successfully")
	return nil
}
