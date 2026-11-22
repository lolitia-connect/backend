package initialize

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

func restoreSentry(svc *svc.ServiceContext) {
	rows, err := svc.Store.System().GetLogConfig(context.Background())
	if err != nil {
		logging.CloseSentry()
		zap.S().Errorw("Unable to load backend Sentry settings", "error", err)
		return
	}
	var settings types.LogSetting
	tool.SystemConfigSliceReflectToStruct(rows, &settings)
	if settings.SentryErrorEnabled == nil {
		settings.SentryErrorEnabled = settings.SentryEnabled
	}
	errorEnabled := settings.SentryErrorEnabled != nil && *settings.SentryErrorEnabled
	logsEnabled := settings.SentryLogsEnabled != nil && *settings.SentryLogsEnabled
	tracingEnabled := settings.SentryTracingEnabled != nil && *settings.SentryTracingEnabled
	metricsEnabled := settings.SentryMetricsEnabled != nil && *settings.SentryMetricsEnabled
	traceIDHeaderEnabled := settings.SentryTraceIDHeaderEnabled != nil && *settings.SentryTraceIDHeaderEnabled
	if err := logging.ConfigureSentryProducts(errorEnabled, logsEnabled, tracingEnabled, metricsEnabled, traceIDHeaderEnabled, settings.SentryDSN, svc.Config.Debug); err != nil {
		logging.CloseSentry()
		zap.S().Warnw("Backend Sentry reporting is disabled", "error", err)
	}
}
