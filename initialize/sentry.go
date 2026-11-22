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
	enabled := settings.SentryEnabled != nil && *settings.SentryEnabled
	if err := logging.ConfigureSentry(enabled, settings.SentryDSN, svc.Config.Debug); err != nil {
		logging.CloseSentry()
		zap.S().Warnw("Backend Sentry reporting is disabled", "error", err)
	}
}
