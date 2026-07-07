package initialize

import (
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

func OAuth(svc *svc.ServiceContext) {
	zap.S().Debug("OAuth config initialization")

}
