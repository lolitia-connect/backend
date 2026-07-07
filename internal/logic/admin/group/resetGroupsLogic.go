package group

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type ResetGroupsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetGroupsLogic Reset all groups (delete all node groups and reset related data)
func NewResetGroupsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetGroupsLogic {
	return &ResetGroupsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetGroupsLogic) ResetGroups() error {
	// 1. Delete all node groups
	err := l.svcCtx.Store.Group().ClearAllNodeGroups(l.ctx)
	if err != nil {
		l.Logger.Errorw("Failed to delete all node groups", zap.Any("error", err.Error()))
		return err
	}
	l.Logger.Infow("Successfully deleted all node groups")

	// 2. Clear node_group_ids for all subscribes (products)
	err = l.svcCtx.Store.Subscribe().ClearAllNodeGroupIds(l.ctx)
	if err != nil {
		l.Logger.Errorw("Failed to clear subscribes' node_group_ids", zap.Any("error", err.Error()))
		return err
	}
	l.Logger.Infow("Successfully cleared all subscribes' node_group_ids")

	// 3. Clear node_group_ids for all nodes
	err = l.svcCtx.Store.Node().ClearAllNodeGroupIds(l.ctx)
	if err != nil {
		l.Logger.Errorw("Failed to clear nodes' node_group_ids", zap.Any("error", err.Error()))
		return err
	}
	l.Logger.Infow("Successfully cleared all nodes' node_group_ids")

	// 4. Clear group history
	err = l.svcCtx.Store.Group().ClearGroupHistories(l.ctx)
	if err != nil {
		l.Logger.Errorw("Failed to clear group history", zap.Any("error", err.Error()))
		// Non-critical error, continue anyway
	} else {
		l.Logger.Infow("Successfully cleared group history")
	}

	// 7. Clear group history details
	err = l.svcCtx.Store.Group().ClearGroupHistoryDetails(l.ctx)
	if err != nil {
		l.Logger.Errorw("Failed to clear group history details", zap.Any("error", err.Error()))
		// Non-critical error, continue anyway
	} else {
		l.Logger.Infow("Successfully cleared group history details")
	}

	// 5. Delete all group config settings
	err = l.svcCtx.Store.System().DeleteByCategory(l.ctx, "group")
	if err != nil {
		l.Logger.Errorw("Failed to delete group config", zap.Any("error", err.Error()))
		return err
	}
	l.Logger.Infow("Successfully deleted all group config settings")

	l.Logger.Infow("Group reset completed successfully")
	return nil
}
