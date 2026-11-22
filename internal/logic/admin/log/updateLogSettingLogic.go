package log

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/perfect-panel/server/internal/config"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logging"
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

// Serialize commits and client swaps so concurrent saves take effect in commit order.
var logSettingsMu sync.Mutex

func (l *UpdateLogSettingLogic) UpdateLogSetting(req *types.LogSetting) error {
	if req == nil || (req.AutoClear == nil && req.SentryEnabled == nil) ||
		(req.AutoClear != nil && req.ClearDays < 7) || (req.AutoClear == nil && req.ClearDays != 0) {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	dsn := strings.TrimSpace(req.SentryDSN)
	if req.SentryEnabled == nil && dsn != "" {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	if req.SentryEnabled != nil && (*req.SentryEnabled || dsn != "") {
		if err := logging.ValidateSentryDSN(dsn); err != nil {
			return xerr.NewErrCodeMsg(xerr.InvalidParams, err.Error())
		}
	}

	logSettingsMu.Lock()
	defer logSettingsMu.Unlock()
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		systemStore := store.System()
		var audit []string
		if req.AutoClear != nil {
			if err := systemStore.UpdateValueByCategoryKey(l.ctx, "log", "AutoClear", strconv.FormatBool(*req.AutoClear)); err != nil {
				return err
			}
			if err := systemStore.UpdateValueByCategoryKey(l.ctx, "log", "ClearDays", strconv.FormatInt(req.ClearDays, 10)); err != nil {
				return err
			}
			audit = append(audit, fmt.Sprintf("auto_clear=%t,clear_days=%d", *req.AutoClear, req.ClearDays))
		}
		if req.SentryEnabled != nil {
			if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryEnabled", strconv.FormatBool(*req.SentryEnabled), "bool", "Backend Sentry reporting"); err != nil {
				return err
			}
			if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryDSN", dsn, "string", "Backend Sentry project DSN"); err != nil {
				return err
			}
			audit = append(audit, fmt.Sprintf("sentry_enabled=%t", *req.SentryEnabled))
		}
		entry, err := modellog.NewAdminActionLog(l.ctx, modellog.AdminAction{Action: "settings.log", Object: "log", Detail: strings.Join(audit, ",")})
		if err != nil {
			return err
		}
		return store.Log().Insert(l.ctx, entry)
	})
	if err != nil {
		l.Logger.Errorw("[UpdateLogSetting] update log setting error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " update log setting error: %v", err)
	}

	if req.AutoClear != nil {
		l.svcCtx.Config.Log = config.Log{AutoClear: *req.AutoClear, ClearDays: req.ClearDays}
	}
	if req.SentryEnabled != nil {
		return logging.ConfigureSentry(*req.SentryEnabled, dsn, l.svcCtx.Config.Debug)
	}
	return nil
}
