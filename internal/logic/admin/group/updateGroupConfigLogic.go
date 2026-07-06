package group

import (
	"context"
	"encoding/json"
	"time"

	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
)

type UpdateGroupConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update group config
func NewUpdateGroupConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateGroupConfigLogic {
	return &UpdateGroupConfigLogic{
		Logger: logger.WithContext(ctx),
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
				l.Errorw("failed to marshal "+key, logger.Field("error", marshalErr.Error()))
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
		l.Errorw("failed to update group config", logger.Field("error", err.Error()))
		return err
	}

	l.Infof("group config updated successfully: enabled=%v, mode=%s", req.Enabled, req.Mode)
	return nil
}

func (l *UpdateGroupConfigLogic) upsertGroupConfig(key, value, desc string) error {
	affected, err := l.svcCtx.Ent.System.Update().Where(
		entsystem.Category("group"),
		entsystem.Key(key),
	).SetValue(value).SetUpdatedAt(time.Now()).Save(l.ctx)
	if err != nil {
		l.Errorw("failed to update group config", logger.Field("key", key), logger.Field("error", err.Error()))
		return err
	}
	if affected > 0 {
		return nil
	}
	now := time.Now()
	return l.svcCtx.Ent.System.Create().
		SetCategory("group").
		SetKey(key).
		SetValue(value).
		SetType("string").
		SetDesc(desc).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Exec(l.ctx)
}
