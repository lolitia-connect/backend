package group

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
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

	// 使用 GORM Transaction 更新配置
	err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 更新 enabled 配置（使用 Upsert 逻辑）
		enabledValue := "false"
		if req.Enabled {
			enabledValue = "true"
		}
		result := tx.Model(&system.System{}).
			Where("`category` = 'group' and `key` = ?", "enabled").
			Update("value", enabledValue)
		if result.Error != nil {
			l.Errorw("failed to update group enabled config", logger.Field("error", result.Error.Error()))
			return result.Error
		}
		// 如果没有更新任何行，说明记录不存在，需要插入
		if result.RowsAffected == 0 {
			if err := tx.Create(&system.System{
				Category: "group",
				Key:      "enabled",
				Value:    enabledValue,
				Desc:     "Group Feature Enabled",
			}).Error; err != nil {
				l.Errorw("failed to create group enabled config", logger.Field("error", err.Error()))
				return err
			}
		}

		// 更新 mode 配置（使用 Upsert 逻辑）
		if req.Mode != "" {
			result := tx.Model(&system.System{}).
				Where("`category` = 'group' and `key` = ?", "mode").
				Update("value", req.Mode)
			if result.Error != nil {
				l.Errorw("failed to update group mode config", logger.Field("error", result.Error.Error()))
				return result.Error
			}
			// 如果没有更新任何行，说明记录不存在，需要插入
			if result.RowsAffected == 0 {
				if err := tx.Create(&system.System{
					Category: "group",
					Key:      "mode",
					Value:    req.Mode,
					Desc:     "Group Mode",
				}).Error; err != nil {
					l.Errorw("failed to create group mode config", logger.Field("error", err.Error()))
					return err
				}
			}
		}

		// 更新 JSON 配置
		if req.Config != nil {
			// 更新 average_config
			if averageConfig, ok := req.Config["average_config"]; ok {
				jsonBytes, err := json.Marshal(averageConfig)
				if err != nil {
					l.Errorw("failed to marshal average_config", logger.Field("error", err.Error()))
					return errors.Wrap(err, "failed to marshal average_config")
				}
				// 使用 Upsert 逻辑：先尝试 UPDATE，如果不存在则 INSERT
				result := tx.Model(&system.System{}).
					Where("`category` = 'group' and `key` = ?", "average_config").
					Update("value", string(jsonBytes))
				if result.Error != nil {
					l.Errorw("failed to update group average_config", logger.Field("error", result.Error.Error()))
					return result.Error
				}
				// 如果没有更新任何行，说明记录不存在，需要插入
				if result.RowsAffected == 0 {
					if err := tx.Create(&system.System{
						Category: "group",
						Key:      "average_config",
						Value:    string(jsonBytes),
						Desc:     "Average Group Config",
					}).Error; err != nil {
						l.Errorw("failed to create group average_config", logger.Field("error", err.Error()))
						return err
					}
				}
			}

			// 更新 subscribe_config
			if subscribeConfig, ok := req.Config["subscribe_config"]; ok {
				jsonBytes, err := json.Marshal(subscribeConfig)
				if err != nil {
					l.Errorw("failed to marshal subscribe_config", logger.Field("error", err.Error()))
					return errors.Wrap(err, "failed to marshal subscribe_config")
				}
				// 使用 Upsert 逻辑：先尝试 UPDATE，如果不存在则 INSERT
				result := tx.Model(&system.System{}).
					Where("`category` = 'group' and `key` = ?", "subscribe_config").
					Update("value", string(jsonBytes))
				if result.Error != nil {
					l.Errorw("failed to update group subscribe_config", logger.Field("error", result.Error.Error()))
					return result.Error
				}
				// 如果没有更新任何行，说明记录不存在，需要插入
				if result.RowsAffected == 0 {
					if err := tx.Create(&system.System{
						Category: "group",
						Key:      "subscribe_config",
						Value:    string(jsonBytes),
						Desc:     "Subscribe Group Config",
					}).Error; err != nil {
						l.Errorw("failed to create group subscribe_config", logger.Field("error", err.Error()))
						return err
					}
				}
			}

			// 更新 traffic_config
			if trafficConfig, ok := req.Config["traffic_config"]; ok {
				jsonBytes, err := json.Marshal(trafficConfig)
				if err != nil {
					l.Errorw("failed to marshal traffic_config", logger.Field("error", err.Error()))
					return errors.Wrap(err, "failed to marshal traffic_config")
				}
				// 使用 Upsert 逻辑：先尝试 UPDATE，如果不存在则 INSERT
				result := tx.Model(&system.System{}).
					Where("`category` = 'group' and `key` = ?", "traffic_config").
					Update("value", string(jsonBytes))
				if result.Error != nil {
					l.Errorw("failed to update group traffic_config", logger.Field("error", result.Error.Error()))
					return result.Error
				}
				// 如果没有更新任何行，说明记录不存在，需要插入
				if result.RowsAffected == 0 {
					if err := tx.Create(&system.System{
						Category: "group",
						Key:      "traffic_config",
						Value:    string(jsonBytes),
						Desc:     "Traffic Group Config",
					}).Error; err != nil {
						l.Errorw("failed to create group traffic_config", logger.Field("error", err.Error()))
						return err
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		l.Errorw("failed to update group config", logger.Field("error", err.Error()))
		return err
	}

	l.Infof("group config updated successfully: enabled=%v, mode=%s", req.Enabled, req.Mode)
	return nil
}
