package initialize

import (
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

func StartInitSystemConfig(svc *svc.ServiceContext) {
	Migrate(svc)
	restoreSentry(svc)
	Site(svc)
	NodeSecret(svc)
	Node(svc)
	Email(svc)
	Device(svc)
	Invite(svc)
	Verify(svc)
	Subscribe(svc)
	Register(svc)
	Mobile(svc)
	Currency(svc)
	if !svc.Config.Debug {
		if err := Telegram(svc); err != nil {
			zap.S().Errorw("Telegram initialization failed", "error", err)
		}
	}

}
