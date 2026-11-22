package logging

import (
	"context"
	"io"
	"strings"

	hertzerrors "github.com/cloudwego/hertz/pkg/common/errors"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"go.uber.org/zap"
)

type HertzLogger struct{}

func NewHertzLogger() HertzLogger {
	return HertzLogger{}
}

func (HertzLogger) Trace(v ...interface{}) {
	zap.S().Debug(v...)
}

func (HertzLogger) Debug(v ...interface{}) {
	zap.S().Debug(v...)
}

func (HertzLogger) Info(v ...interface{}) {
	zap.S().Info(v...)
}

func (HertzLogger) Notice(v ...interface{}) {
	zap.S().Info(v...)
}

func (HertzLogger) Warn(v ...interface{}) {
	zap.S().Warn(v...)
}

func (HertzLogger) Error(v ...interface{}) {
	zap.S().Error(v...)
}

func (HertzLogger) Fatal(v ...interface{}) {
	zap.S().Fatal(v...)
}

func (HertzLogger) Tracef(format string, v ...interface{}) {
	zap.S().Debugf(format, v...)
}

func (HertzLogger) Debugf(format string, v ...interface{}) {
	zap.S().Debugf(format, v...)
}

func (HertzLogger) Infof(format string, v ...interface{}) {
	zap.S().Infof(format, v...)
}

func (HertzLogger) Noticef(format string, v ...interface{}) {
	zap.S().Infof(format, v...)
}

func (HertzLogger) Warnf(format string, v ...interface{}) {
	zap.S().Warnf(format, v...)
}

func (HertzLogger) Errorf(format string, v ...interface{}) {
	// Hertz passes err.Error() here, after discarding the parser's error type.
	if (format == "HERTZ: "+hlog.EngineErrorFormat || format == hlog.EngineErrorFormat) && len(v) == 2 {
		if reason, ok := v[0].(string); ok && isRejectedRequest(reason) {
			zap.S().Debug("HERTZ: rejected invalid HTTP request")
			return
		}
	}
	zap.S().Errorf(format, v...)
}

func isRejectedRequest(reason string) bool {
	if reason == hertzerrors.ErrHeaderTooLarge.Error() || reason == hertzerrors.ErrBodyTooLarge.Error() {
		return true
	}
	header, ok := strings.CutPrefix(reason, "error when reading request headers: ")
	if !ok {
		return false
	}
	cause, _, _ := strings.Cut(header, ". Buffer size=")
	switch cause {
	case "malformed HTTP request", "invalid header name", "non-numeric chars at the end of Content-Length",
		"EOF", "unexpected EOF", "timeout", "duplicate Content-Length header with conflicting values", "multiple Transfer-Encoding headers are not allowed":
		return true
	}
	return strings.HasPrefix(cause, "invalid header key ") || strings.HasPrefix(cause, "invalid header value ") ||
		strings.HasPrefix(cause, "unsupported Transfer-Encoding:") || strings.HasPrefix(cause, "both Transfer-Encoding and Content-Length headers are present in request:")
}

func (HertzLogger) Fatalf(format string, v ...interface{}) {
	zap.S().Fatalf(format, v...)
}

func (l HertzLogger) CtxTracef(_ context.Context, format string, v ...interface{}) {
	l.Tracef(format, v...)
}

func (l HertzLogger) CtxDebugf(_ context.Context, format string, v ...interface{}) {
	l.Debugf(format, v...)
}

func (l HertzLogger) CtxInfof(_ context.Context, format string, v ...interface{}) {
	l.Infof(format, v...)
}

func (l HertzLogger) CtxNoticef(_ context.Context, format string, v ...interface{}) {
	l.Noticef(format, v...)
}

func (l HertzLogger) CtxWarnf(_ context.Context, format string, v ...interface{}) {
	l.Warnf(format, v...)
}

func (l HertzLogger) CtxErrorf(_ context.Context, format string, v ...interface{}) {
	l.Errorf(format, v...)
}

func (l HertzLogger) CtxFatalf(_ context.Context, format string, v ...interface{}) {
	l.Fatalf(format, v...)
}

func (HertzLogger) SetLevel(hlog.Level) {
}

func (HertzLogger) SetOutput(io.Writer) {
}
