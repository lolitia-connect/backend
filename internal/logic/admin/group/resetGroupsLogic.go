package group

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
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
	return withGroupLock(l.ctx, l.svcCtx, func(ctx context.Context) error {
		return l.svcCtx.Store.InTx(ctx, func(store repository.Store) error {
			if err := store.Group().ClearAllNodeGroups(ctx); err != nil {
				return err
			}
			if err := store.Subscribe().ClearAllNodeGroupIds(ctx); err != nil {
				return err
			}
			if err := store.Node().ClearAllNodeGroupIds(ctx); err != nil {
				return err
			}
			if err := store.Group().ClearGroupHistoryDetails(ctx); err != nil {
				return err
			}
			if err := store.Group().ClearGroupHistories(ctx); err != nil {
				return err
			}
			return store.System().DeleteByCategory(ctx, "group")
		})
	})
}
