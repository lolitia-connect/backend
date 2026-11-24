package group

import (
	"context"
	"github.com/perfect-panel/server/ent"
	queueTypes "github.com/perfect-panel/server/queue/types"

	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"

	"github.com/hibiken/asynq"
)

type RecalculateGroupLogic struct {
	svc *svc.ServiceContext
}

func NewRecalculateGroupLogic(svc *svc.ServiceContext) *RecalculateGroupLogic {
	return &RecalculateGroupLogic{
		svc: svc,
	}
}

func (l *RecalculateGroupLogic) ProcessTask(ctx context.Context, t *asynq.Task) error {
	orderTriggered := t.Type() == queueTypes.OrderRecalculateGroup

	// 1. Check if group management is enabled
	enabledConfig, err := l.svc.Ent.System.Query().
		Where(entsystem.Category("group"), entsystem.Key("enabled")).
		Only(ctx)
	if orderTriggered && ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		zap.S().Errorw("[RecalculateGroup] Failed to read group enabled config", zap.Any("error", err.Error()))
		return err
	}

	// If not enabled, skip execution
	if enabledConfig.Value != "true" && enabledConfig.Value != "1" {
		zap.S().Debugf("[RecalculateGroup] Group management is not enabled, skipping")
		return nil
	}

	// 2. Get grouping mode
	modeConfig, err := l.svc.Ent.System.Query().
		Where(entsystem.Category("group"), entsystem.Key("mode")).
		Only(ctx)
	if err != nil {
		zap.S().Errorw("[RecalculateGroup] Failed to read group mode config", zap.Any("error", err.Error()))
		return err
	}

	mode := modeConfig.Value
	trigger := "scheduled"
	if orderTriggered {
		if mode != "average" && mode != "subscribe" && mode != "traffic" {
			return nil
		}
		trigger = "manual" // Preserve the history label for order-triggered runs.
	} else if mode != "traffic" {
		return nil
	}
	return group.NewRecalculateGroupLogic(ctx, l.svc).RecalculateGroup(&types.RecalculateGroupRequest{Mode: mode, TriggerType: trigger})
}
