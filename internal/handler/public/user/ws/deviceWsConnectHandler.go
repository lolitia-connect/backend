package ws

import (
	"errors"

	wslogic "github.com/perfect-panel/server/internal/logic/public/user/ws"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Webosocket Device Connect
func DeviceWsConnectHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {

		l := wslogic.NewDeviceWsConnectLogic(c.Request.Context(), svcCtx)
		err := l.DeviceWsConnect(c)
		if err != nil {
			var handshake *device.HandshakeError
			if errors.As(err, &handshake) {
				c.String(handshake.Status, "%s", handshake.Reason)
				return
			}
			result.HttpResult(c, nil, err)
		}
	}
}
