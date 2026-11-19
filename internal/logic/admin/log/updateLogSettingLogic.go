package log

import (
	"context"
	"fmt"
	"reflect"

	"github.com/perfect-panel/server/internal/config"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateLogSettingLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateLogSettingLogic Update log setting
func NewUpdateLogSettingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateLogSettingLogic {
	return &UpdateLogSettingLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateLogSettingLogic) UpdateLogSetting(req *types.LogSetting) error {
	if req == nil || req.AutoClear == nil || req.ClearDays < 7 {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	v := reflect.ValueOf(*req)
	// Get the reflection type of the structure
	t := v.Type()
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		systemStore := store.System()
		for i := 0; i < v.NumField(); i++ {
			// Get the field name
			fieldName := t.Field(i).Name
			// Get the field value to string
			fieldValue := tool.ConvertValueToString(v.Field(i))
			if err := systemStore.UpdateValueByCategoryKey(l.ctx, "log", fieldName, fieldValue); err != nil {
				return err
			}
		}
		entry, err := modellog.NewAdminActionLog(l.ctx, modellog.AdminAction{Action: "settings.log", Object: "log", Detail: fmt.Sprintf("auto_clear=%t,clear_days=%d", *req.AutoClear, req.ClearDays)})
		if err != nil {
			return err
		}
		return store.Log().Insert(l.ctx, entry)
	})
	if err != nil {
		l.Logger.Errorw("[UpdateLogSetting] update log setting error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " update log setting error: %v", err)
	}

	l.svcCtx.Config.Log = config.Log{
		AutoClear: *req.AutoClear,
		ClearDays: req.ClearDays,
	}

	return nil
}
