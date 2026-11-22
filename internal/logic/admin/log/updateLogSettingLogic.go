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
	if req == nil {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	sentryErrorEnabled := req.SentryErrorEnabled
	if sentryErrorEnabled == nil {
		sentryErrorEnabled = req.SentryEnabled
	}
	sentryRequested := sentryErrorEnabled != nil || req.SentryLogsEnabled != nil || req.SentryTracingEnabled != nil || req.SentryMetricsEnabled != nil || req.SentryTraceIDHeaderEnabled != nil || req.SentryDSN != ""
	if req == nil || (req.AutoClear == nil && !sentryRequested) ||
		(req.AutoClear != nil && req.ClearDays < 7) || (req.AutoClear == nil && req.ClearDays != 0) {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	dsn := strings.TrimSpace(req.SentryDSN)
	if !sentryRequested && dsn != "" {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	if sentryRequested && (sentryErrorEnabled != nil && *sentryErrorEnabled || req.SentryLogsEnabled != nil && *req.SentryLogsEnabled || req.SentryTracingEnabled != nil && *req.SentryTracingEnabled || req.SentryMetricsEnabled != nil && *req.SentryMetricsEnabled || dsn != "") {
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
		if sentryRequested {
			if sentryErrorEnabled != nil {
				value := strconv.FormatBool(*sentryErrorEnabled)
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryEnabled", value, "bool", "Backend Sentry error monitoring"); err != nil {
					return err
				}
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryErrorEnabled", value, "bool", "Backend Sentry error monitoring"); err != nil {
					return err
				}
			}
			if req.SentryLogsEnabled != nil {
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryLogsEnabled", strconv.FormatBool(*req.SentryLogsEnabled), "bool", "Backend Sentry logging"); err != nil {
					return err
				}
			}
			if req.SentryTracingEnabled != nil {
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryTracingEnabled", strconv.FormatBool(*req.SentryTracingEnabled), "bool", "Backend Sentry tracing"); err != nil {
					return err
				}
			}
			if req.SentryMetricsEnabled != nil {
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryMetricsEnabled", strconv.FormatBool(*req.SentryMetricsEnabled), "bool", "Backend Sentry application metrics"); err != nil {
					return err
				}
			}
			if req.SentryTraceIDHeaderEnabled != nil {
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryTraceIDHeaderEnabled", strconv.FormatBool(*req.SentryTraceIDHeaderEnabled), "bool", "Backend Sentry trace ID response header"); err != nil {
					return err
				}
			}
			if req.SentryDSN != "" || sentryErrorEnabled != nil {
				if err := systemStore.UpsertByCategoryKey(l.ctx, "log", "SentryDSN", dsn, "string", "Backend Sentry project DSN"); err != nil {
					return err
				}
			}
			audit = append(audit, fmt.Sprintf("sentry_error=%t,logs=%t,tracing=%t,metrics=%t,trace_id_header=%t", boolValue(sentryErrorEnabled), boolValue(req.SentryLogsEnabled), boolValue(req.SentryTracingEnabled), boolValue(req.SentryMetricsEnabled), boolValue(req.SentryTraceIDHeaderEnabled)))
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
	if sentryRequested {
		return logging.ConfigureSentryProducts(boolValue(sentryErrorEnabled), boolValue(req.SentryLogsEnabled), boolValue(req.SentryTracingEnabled), boolValue(req.SentryMetricsEnabled), boolValue(req.SentryTraceIDHeaderEnabled), dsn, l.svcCtx.Config.Debug)
	}
	return nil
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
