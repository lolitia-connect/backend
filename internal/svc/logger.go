package svc

import (
	"github.com/perfect-panel/server/internal/config"
	"go.uber.org/zap"
)

func NewLogger(c config.Config) **zap.SugaredLogger {
	//log := logger.New(c.Logger)
	//// replace the default logger
	//logger = log
	//return log
	return nil
}
