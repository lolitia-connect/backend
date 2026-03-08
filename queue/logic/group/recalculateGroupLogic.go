package group

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"

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
	logger.Infof("[RecalculateGroup] Starting scheduled group recalculation: %s", time.Now().Format("2006-01-02 15:04:05"))

	// 1. Check if group management is enabled
	var enabledConfig struct {
		Value string `gorm:"column:value"`
	}
	err := l.svc.DB.Table("system").
		Where("`category` = ? AND `key` = ?", "group", "enabled").
		Select("value").
		First(&enabledConfig).Error
	if err != nil {
		logger.Errorw("[RecalculateGroup] Failed to read group enabled config", logger.Field("error", err.Error()))
		return err
	}

	// If not enabled, skip execution
	if enabledConfig.Value != "true" && enabledConfig.Value != "1" {
		logger.Debugf("[RecalculateGroup] Group management is not enabled, skipping")
		return nil
	}

	// 2. Get grouping mode
	var modeConfig struct {
		Value string `gorm:"column:value"`
	}
	err = l.svc.DB.Table("system").
		Where("`category` = ? AND `key` = ?", "group", "mode").
		Select("value").
		First(&modeConfig).Error
	if err != nil {
		logger.Errorw("[RecalculateGroup] Failed to read group mode config", logger.Field("error", err.Error()))
		return err
	}

	mode := modeConfig.Value
	if mode == "" {
		mode = "average" // default mode
	}

	// 3. Only execute if mode is "traffic"
	if mode != "traffic" {
		logger.Debugf("[RecalculateGroup] Group mode is not 'traffic' (current: %s), skipping", mode)
		return nil
	}

	// 4. Execute traffic-based grouping
	logger.Infof("[RecalculateGroup] Executing traffic-based grouping")

	logic := group.NewRecalculateGroupLogic(ctx, l.svc)
	req := &types.RecalculateGroupRequest{
		Mode:        "traffic",
		TriggerType: "scheduled",
	}

	if err := logic.RecalculateGroup(req); err != nil {
		logger.Errorw("[RecalculateGroup] Failed to execute traffic grouping", logger.Field("error", err.Error()))
		return err
	}

	logger.Infof("[RecalculateGroup] Successfully completed traffic-based grouping: %s", time.Now().Format("2006-01-02 15:04:05"))
	return nil
}
