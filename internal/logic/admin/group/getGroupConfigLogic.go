package group

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/grouphistory"
	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
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
	enabledConfig, err := l.svcCtx.Ent.System.Query().Where(entsystem.Category("group"), entsystem.Key("enabled")).Only(l.ctx)
	if err != nil && !ent.IsNotFound(err) {
		l.Errorw("failed to get group enabled config", logger.Field("error", err.Error()))
		return nil, err
	}

	modeConfig, err := l.svcCtx.Ent.System.Query().Where(entsystem.Category("group"), entsystem.Key("mode")).Only(l.ctx)
	if err != nil && !ent.IsNotFound(err) {
		l.Errorw("failed to get group mode config", logger.Field("error", err.Error()))
		return nil, err
	}

	// 读取 JSON 配置
	config := make(map[string]interface{})

	if averageConfig, err := l.svcCtx.Ent.System.Query().Where(entsystem.Category("group"), entsystem.Key("average_config")).Only(l.ctx); err == nil {
		var averageCfg map[string]interface{}
		if err := json.Unmarshal([]byte(averageConfig.Value), &averageCfg); err == nil {
			config["average_config"] = averageCfg
		}
	}

	if subscribeConfig, err := l.svcCtx.Ent.System.Query().Where(entsystem.Category("group"), entsystem.Key("subscribe_config")).Only(l.ctx); err == nil {
		var subscribeCfg map[string]interface{}
		if err := json.Unmarshal([]byte(subscribeConfig.Value), &subscribeCfg); err == nil {
			config["subscribe_config"] = subscribeCfg
		}
	}

	if trafficConfig, err := l.svcCtx.Ent.System.Query().Where(entsystem.Category("group"), entsystem.Key("traffic_config")).Only(l.ctx); err == nil {
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
	history, err := l.svcCtx.Ent.GroupHistory.Query().Order(ent.Desc(grouphistory.FieldID)).First(l.ctx)
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
