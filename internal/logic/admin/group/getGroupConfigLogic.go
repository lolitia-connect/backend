package group

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type GetGroupConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get group config
func NewGetGroupConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroupConfigLogic {
	return &GetGroupConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroupConfigLogic) GetGroupConfig(req *types.GetGroupConfigRequest) (resp *types.GetGroupConfigResponse, err error) {
	// 读取基础配置
	var enabledConfig system.System
	var modeConfig system.System
	var averageConfig system.System
	var subscribeConfig system.System
	var trafficConfig system.System

	// 从 system_config 表读取配置
	if err := l.svcCtx.DB.Where("`category` = 'group' and `key` = ?", "enabled").First(&enabledConfig).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("failed to get group enabled config", logger.Field("error", err.Error()))
		return nil, err
	}

	if err := l.svcCtx.DB.Where("`category` = 'group' and `key` = ?", "mode").First(&modeConfig).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("failed to get group mode config", logger.Field("error", err.Error()))
		return nil, err
	}

	// 读取 JSON 配置
	config := make(map[string]interface{})

	if err := l.svcCtx.DB.Where("`category` = 'group' and `key` = ?", "average_config").First(&averageConfig).Error; err == nil {
		var averageCfg map[string]interface{}
		if err := json.Unmarshal([]byte(averageConfig.Value), &averageCfg); err == nil {
			config["average_config"] = averageCfg
		}
	}

	if err := l.svcCtx.DB.Where("`category` = 'group' and `key` = ?", "subscribe_config").First(&subscribeConfig).Error; err == nil {
		var subscribeCfg map[string]interface{}
		if err := json.Unmarshal([]byte(subscribeConfig.Value), &subscribeCfg); err == nil {
			config["subscribe_config"] = subscribeCfg
		}
	}

	if err := l.svcCtx.DB.Where("`category` = 'group' and `key` = ?", "traffic_config").First(&trafficConfig).Error; err == nil {
		var trafficCfg map[string]interface{}
		if err := json.Unmarshal([]byte(trafficConfig.Value), &trafficCfg); err == nil {
			config["traffic_config"] = trafficCfg
		}
	}

	// 解析基础配置
	enabled := enabledConfig.Value == "true"
	mode := modeConfig.Value
	if mode == "" {
		mode = "average" // 默认模式
	}

	// 获取重算状态
	state, err := l.getRecalculationState()
	if err != nil {
		l.Errorw("failed to get recalculation state", logger.Field("error", err.Error()))
		// 继续执行，不影响配置获取
		state = &types.RecalculationState{
			State:    "idle",
			Progress: 0,
			Total:    0,
		}
	}

	resp = &types.GetGroupConfigResponse{
		Enabled: enabled,
		Mode:    mode,
		Config:  config,
		State:   *state,
	}

	return resp, nil
}

// getRecalculationState 获取重算状态
func (l *GetGroupConfigLogic) getRecalculationState() (*types.RecalculationState, error) {
	var history group.GroupHistory
	err := l.svcCtx.DB.Order("id desc").First(&history).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &types.RecalculationState{
				State:    "idle",
				Progress: 0,
				Total:    0,
			}, nil
		}
		return nil, err
	}

	state := &types.RecalculationState{
		State:    history.State,
		Progress: history.TotalUsers,
		Total:    history.TotalUsers,
	}

	return state, nil
}
