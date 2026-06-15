package user

import (
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Device Online Statistics
func DeviceOnlineStatisticsHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {

		l := user.NewDeviceOnlineStatisticsLogic(c.Request.Context(), svcCtx)
		resp, err := l.DeviceOnlineStatistics()
		result.HttpResult(c, resp, err)
	}
}
