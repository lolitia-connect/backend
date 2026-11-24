package group

import (
	"context"
	"encoding/json"
	"github.com/perfect-panel/server/internal/repository"
	"strconv"

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

	return l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		upsert := func(key, value, desc string) error {
			return store.System().UpsertByCategoryKey(l.ctx, "group", key, value, "string", desc)
		}
		if err := upsert("enabled", strconv.FormatBool(req.Enabled), "Group Feature Enabled"); err != nil {
			return err
		}
		if req.Mode != "" {
			if err := upsert("mode", req.Mode, "Group Mode"); err != nil {
				return err
			}
		}
		for _, setting := range []struct{ key, desc string }{
			{"average_config", "Average Group Config"}, {"subscribe_config", "Subscribe Group Config"}, {"traffic_config", "Traffic Group Config"},
		} {
			config, ok := req.Config[setting.key]
			if !ok {
				continue
			}
			data, err := json.Marshal(config)
			if err != nil {
				return errors.Wrap(err, "marshal "+setting.key)
			}
			if err := upsert(setting.key, string(data), setting.desc); err != nil {
				return err
			}
		}
		return nil
	})
}
