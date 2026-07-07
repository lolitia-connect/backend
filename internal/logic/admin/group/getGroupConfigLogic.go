package group

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetGroupConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get group config
func NewGetGroupConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroupConfigLogic {
	return &GetGroupConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroupConfigLogic) GetGroupConfig(req *types.GetGroupConfigRequest) (resp *types.GetGroupConfigResponse, err error) {
	// 读取基础配置
	enabledConfig, err := l.svcCtx.Store.System().FindByCategoryKey(l.ctx, "group", "enabled")
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("failed to get group enabled config", zap.Any("error", err.Error()))
		return nil, err
	}

	modeConfig, err := l.svcCtx.Store.System().FindByCategoryKey(l.ctx, "group", "mode")
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("failed to get group mode config", zap.Any("error", err.Error()))
		return nil, err
	}

	// 读取 JSON 配置
	config := make(map[string]interface{})

	if averageConfig, err := l.svcCtx.Store.System().FindByCategoryKey(l.ctx, "group", "average_config"); err == nil {
		var averageCfg map[string]interface{}
		if err := json.Unmarshal([]byte(averageConfig.Value), &averageCfg); err == nil {
			config["average_config"] = averageCfg
		}
	}

	if subscribeConfig, err := l.svcCtx.Store.System().FindByCategoryKey(l.ctx, "group", "subscribe_config"); err == nil {
		var subscribeCfg map[string]interface{}
		if err := json.Unmarshal([]byte(subscribeConfig.Value), &subscribeCfg); err == nil {
			config["subscribe_config"] = subscribeCfg
		}
	}

	if trafficConfig, err := l.svcCtx.Store.System().FindByCategoryKey(l.ctx, "group", "traffic_config"); err == nil {
		var trafficCfg map[string]interface{}
		if err := json.Unmarshal([]byte(trafficConfig.Value), &trafficCfg); err == nil {
			config["traffic_config"] = trafficCfg
		}
	}

	// 解析基础配置
	enabled := enabledConfig != nil && enabledConfig.Value == "true"
	mode := ""
	if modeConfig != nil {
		mode = modeConfig.Value
	}
	if mode == "" {
		mode = "average" // 默认模式
	}

	// 获取重算状态
	state, err := l.getRecalculationState()
	if err != nil {
		l.Logger.Errorw("failed to get recalculation state", zap.Any("error", err.Error()))
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
	history, err := l.svcCtx.Store.Group().FindLatestGroupHistory(l.ctx)
	if err != nil {
		if ent.IsNotFound(err) {
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
