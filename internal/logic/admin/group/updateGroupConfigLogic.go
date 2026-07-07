package group

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateGroupConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update group config
func NewUpdateGroupConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateGroupConfigLogic {
	return &UpdateGroupConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateGroupConfigLogic) UpdateGroupConfig(req *types.UpdateGroupConfigRequest) error {
	// 验证 mode 是否为合法值
	if req.Mode != "" {
		if req.Mode != "average" && req.Mode != "subscribe" && req.Mode != "traffic" {
			return errors.New("invalid mode, must be one of: average, subscribe, traffic")
		}
	}

	enabledValue := "false"
	if req.Enabled {
		enabledValue = "true"
	}
	err := l.upsertGroupConfig("enabled", enabledValue, "Group Feature Enabled")
	if err == nil && req.Mode != "" {
		err = l.upsertGroupConfig("mode", req.Mode, "Group Mode")
	}
	if err == nil && req.Config != nil {
		configDescs := map[string]string{
			"average_config":   "Average Group Config",
			"subscribe_config": "Subscribe Group Config",
			"traffic_config":   "Traffic Group Config",
		}
		for key, desc := range configDescs {
			config, ok := req.Config[key]
			if !ok {
				continue
			}
			jsonBytes, marshalErr := json.Marshal(config)
			if marshalErr != nil {
				l.Logger.Errorw("failed to marshal "+key, zap.Any("error", marshalErr.Error()))
				err = errors.Wrap(marshalErr, "failed to marshal "+key)
				break
			}
			err = l.upsertGroupConfig(key, string(jsonBytes), desc)
			if err != nil {
				break
			}
		}
	}

	if err != nil {
		l.Logger.Errorw("failed to update group config", zap.Any("error", err.Error()))
		return err
	}

	l.Logger.Infof("group config updated successfully: enabled=%v, mode=%s", req.Enabled, req.Mode)
	return nil
}

func (l *UpdateGroupConfigLogic) upsertGroupConfig(key, value, desc string) error {
	return l.svcCtx.Store.System().UpsertByCategoryKey(l.ctx, "group", key, value, "string", desc)
}
