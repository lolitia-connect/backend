package logging

import (
	"go.uber.org/zap"
)

type AsynqLogger struct{}

func NewAsynqLogger() AsynqLogger {
	return AsynqLogger{}
}

func (AsynqLogger) Debug(args ...interface{}) {
	zap.S().Debug(args...)
}

func (AsynqLogger) Info(args ...interface{}) {
	zap.S().Info(args...)
}

func (AsynqLogger) Warn(args ...interface{}) {
	zap.S().Warn(args...)
}

func (AsynqLogger) Error(args ...interface{}) {
	zap.S().Error(args...)
}

func (AsynqLogger) Fatal(args ...interface{}) {
	zap.S().Fatal(args...)
}
