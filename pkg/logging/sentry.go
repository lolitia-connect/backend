package logging

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logredact"
	"go.uber.org/zap/zapcore"
)

const sentryFlushTimeout = 2 * time.Second

var (
	backendSentry sentryCore
	errInvalidDSN = errors.New("a public HTTP(S) Sentry DSN with a numeric project ID is required")
)

func ValidateSentryDSN(raw string) error {
	dsn, err := sentry.NewDsn(raw)
	if err != nil || strings.ContainsAny(raw, "?#\r\n\t ") || dsn.GetSecretKey() != "" || dsn.GetPort() < 1 || dsn.GetPort() > 65535 {
		return errInvalidDSN
	}
	if _, err := strconv.ParseUint(dsn.GetProjectID(), 10, 64); err != nil {
		return errInvalidDSN
	}
	return nil
}

func ConfigureSentry(enabled bool, dsn string, debug bool) error {
	return ConfigureSentryProducts(enabled, enabled, enabled, enabled, false, dsn, debug)
}

func ConfigureSentryProducts(errorEnabled, logsEnabled, tracingEnabled, metricsEnabled, traceIDHeaderEnabled bool, dsn string, debug bool) error {
	enabled := errorEnabled || logsEnabled || tracingEnabled || metricsEnabled || traceIDHeaderEnabled
	if !enabled {
		CloseSentry()
		return nil
	}
	if err := ValidateSentryDSN(dsn); err != nil {
		return err
	}
	environment := "production"
	if debug {
		environment = "development"
	}
	backendSentry.mu.Lock()
	old := backendSentry.client
	if old != nil && old.Options().Dsn == dsn && old.Options().Environment == environment &&
		backendSentry.errorEnabled == errorEnabled && backendSentry.logsEnabled == logsEnabled &&
		backendSentry.tracingEnabled == tracingEnabled && backendSentry.metricsEnabled == metricsEnabled &&
		backendSentry.traceIDHeaderEnabled == traceIDHeaderEnabled {
		backendSentry.mu.Unlock()
		return nil
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:                   dsn,
		Release:               constant.Version,
		Environment:           environment,
		EnableTracing:         tracingEnabled,
		TracesSampleRate:      1.0,
		MaxBreadcrumbs:        -1,
		DisableClientReports:  true,
		Integrations:          func([]sentry.Integration) []sentry.Integration { return nil },
		BeforeSend:            safeBackendEvent,
		BeforeSendLog:         safeBackendLog,
		BeforeSendTransaction: safeBackendTransaction,
		BeforeSendMetric:      safeBackendMetric,
	})
	if err != nil {
		backendSentry.mu.Unlock()
		return errInvalidDSN
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	backendSentry.client = client
	backendSentry.hub = hub
	backendSentry.errorEnabled = errorEnabled
	backendSentry.logsEnabled = logsEnabled
	backendSentry.tracingEnabled = tracingEnabled
	backendSentry.metricsEnabled = metricsEnabled
	backendSentry.traceIDHeaderEnabled = traceIDHeaderEnabled
	backendSentry.mu.Unlock()
	closeClient(old)
	return nil
}

func CloseSentry() {
	backendSentry.mu.Lock()
	old := backendSentry.client
	backendSentry.client = nil
	backendSentry.hub = nil
	backendSentry.errorEnabled = false
	backendSentry.logsEnabled = false
	backendSentry.tracingEnabled = false
	backendSentry.metricsEnabled = false
	backendSentry.traceIDHeaderEnabled = false
	backendSentry.mu.Unlock()
	closeClient(old)
}

func closeClient(client *sentry.Client) {
	if client != nil {
		client.Flush(sentryFlushTimeout)
		client.Close()
	}
}

// The local core owns log messages and fields. They can contain credentials,
// request bodies or SQL, including when interpolated into the message itself.
type sentryCore struct {
	mu                   sync.RWMutex
	client               *sentry.Client
	hub                  *sentry.Hub
	errorEnabled         bool
	logsEnabled          bool
	tracingEnabled       bool
	metricsEnabled       bool
	traceIDHeaderEnabled bool
}

func (c *sentryCore) Enabled(level zapcore.Level) bool {
	if level < zapcore.InfoLevel {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.client == nil || c.hub == nil {
		return false
	}
	return (level >= zapcore.ErrorLevel && c.errorEnabled) || c.logsEnabled
}

func (c *sentryCore) With([]zapcore.Field) zapcore.Core { return c }
func (c *sentryCore) Sync() error                       { return nil }
func (c *sentryCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *sentryCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	c.mu.RLock()
	client, hub := c.client, c.hub
	errorEnabled, logsEnabled := c.errorEnabled, c.logsEnabled
	c.mu.RUnlock()
	if client == nil || hub == nil {
		return nil
	}

	if entry.Level >= zapcore.ErrorLevel && errorEnabled {
		level := sentry.LevelError
		if entry.Level >= zapcore.PanicLevel {
			level = sentry.LevelFatal
		}
		client.CaptureEvent(&sentry.Event{
			Level:     level,
			Timestamp: entry.Time,
			Exception: []sentry.Exception{{Stacktrace: sentry.NewStacktrace()}},
		}, nil, nil)
	}

	if !logsEnabled {
		return nil
	}
	logger := sentry.NewLogger(sentry.SetHubOnContext(context.Background(), hub))
	logEntry := logger.Info()
	switch {
	case entry.Level >= zapcore.PanicLevel:
		logEntry = logger.LFatal()
	case entry.Level >= zapcore.ErrorLevel:
		logEntry = logger.Error()
	case entry.Level >= zapcore.WarnLevel:
		logEntry = logger.Warn()
	case entry.Level >= zapcore.InfoLevel:
		logEntry = logger.Info()
	default:
		logEntry = logger.Debug()
	}
	logEntry.String("logger", logredact.Text(entry.LoggerName)).String("caller", filepath.Base(entry.Caller.File))
	if len(fields) > 0 {
		encoded := zapcore.NewMapObjectEncoder()
		for _, field := range fields {
			field.AddTo(encoded)
		}
		if value, err := json.Marshal(logredact.Value(encoded.Fields)); err == nil {
			logEntry.String("fields", string(value))
		}
	}
	logEntry.Emit(logredact.Text(entry.Message))
	return nil
}

func safeBackendEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	// Keep only symbol locations for grouping. SDK enrichment (including the
	// machine name) and source text must not cross the reporting boundary.
	clean := &sentry.Event{
		EventID:     event.EventID,
		Level:       event.Level,
		Timestamp:   event.Timestamp,
		Environment: event.Environment,
		Release:     event.Release,
		Sdk:         event.Sdk,
		Platform:    event.Platform,
	}
	exception := sentry.Exception{Type: "Backend error", Value: "Backend error"}
	if len(event.Exception) != 0 && event.Exception[0].Stacktrace != nil {
		stack := &sentry.Stacktrace{}
		for _, frame := range event.Exception[0].Stacktrace.Frames {
			if strings.HasPrefix(frame.Module, "go.uber.org/zap") || (frame.Module == "github.com/perfect-panel/server/pkg/logging" && frame.Function == "(*sentryCore).Write") {
				continue
			}
			file := frame.Filename
			if file == "" {
				file = frame.AbsPath
			}
			stack.Frames = append(stack.Frames, sentry.Frame{
				Module:   frame.Module,
				Function: frame.Function,
				Filename: filepath.Base(file),
				Lineno:   frame.Lineno,
				InApp:    strings.HasPrefix(frame.Module, "github.com/perfect-panel/server/"),
			})
		}
		exception.Stacktrace = stack
	}
	clean.Exception = []sentry.Exception{exception}
	return clean
}

func safeBackendLog(event *sentry.Log) *sentry.Log {
	if event == nil {
		return nil
	}
	event.Body = logredact.Text(event.Body)
	attrs := make(map[string]attribute.Value, len(event.Attributes))
	for key, value := range event.Attributes {
		if logredact.SensitiveKey(key) {
			continue
		}
		attrs[key] = safeAttributeValue(value)
	}
	event.Attributes = attrs
	return event
}

func safeBackendMetric(metric *sentry.Metric) *sentry.Metric {
	if metric == nil {
		return nil
	}
	attrs := make(map[string]attribute.Value, len(metric.Attributes))
	for key, value := range metric.Attributes {
		if logredact.SensitiveKey(key) {
			continue
		}
		attrs[key] = safeAttributeValue(value)
	}
	metric.Attributes = attrs
	return metric
}

func safeBackendTransaction(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}
	event.Request = nil
	event.User = sentry.User{}
	event.ServerName = ""
	event.Breadcrumbs = nil
	event.Tags = nil
	traceContext := make(map[string]sentry.Context, 1)
	if event.Contexts != nil {
		if trace, ok := event.Contexts["trace"]; ok {
			traceContext["trace"] = trace
		}
	}
	event.Contexts = traceContext
	return event
}

func safeAttributeValue(value attribute.Value) attribute.Value {
	switch value.Type() {
	case attribute.STRING:
		return attribute.StringValue(logredact.Text(value.AsString()))
	case attribute.STRINGSLICE:
		values := value.AsStringSlice()
		redacted := make([]string, len(values))
		for i, item := range values {
			redacted[i] = logredact.Text(item)
		}
		return attribute.StringSliceValue(redacted)
	default:
		return value
	}
}

func StartSentryRequest(ctx context.Context, name string) (context.Context, func(status int, expected bool), string) {
	backendSentry.mu.RLock()
	hub := backendSentry.hub
	tracingEnabled := backendSentry.tracingEnabled
	traceIDHeaderEnabled := backendSentry.traceIDHeaderEnabled
	backendSentry.mu.RUnlock()
	if hub == nil || (!tracingEnabled && !traceIDHeaderEnabled) {
		return ctx, func(int, bool) {}, ""
	}

	ctx = sentry.SetHubOnContext(ctx, hub)
	span := sentry.StartSpan(
		ctx,
		"http.server",
		sentry.WithTransactionName(logredact.Text(name)),
		sentry.WithDescription(logredact.Text(name)),
	)
	traceID := ""
	if traceIDHeaderEnabled {
		traceID = span.TraceID.String()
	}
	return span.Context(), func(status int, expected bool) {
		span.SetTag("http.status_code", strconv.Itoa(status))
		if expected {
			span.Status = sentry.HTTPtoSpanStatus(status)
		} else {
			span.Status = sentry.SpanStatusInternalError
		}
		span.Finish()
	}, traceID
}

func RecordSentryRequestMetrics(ctx context.Context, method, route string, status int, duration time.Duration) {
	backendSentry.mu.RLock()
	hub := backendSentry.hub
	metricsEnabled := backendSentry.metricsEnabled
	backendSentry.mu.RUnlock()
	if hub == nil || !metricsEnabled {
		return
	}

	if route == "" {
		route = "unknown"
	}
	statusClass := "unknown"
	if status >= 100 && status < 600 {
		statusClass = strconv.Itoa(status/100) + "xx"
	}
	attrs := []attribute.Builder{
		attribute.String("http.method", logredact.Text(method)),
		attribute.String("http.route", logredact.Text(route)),
		attribute.String("http.status_class", statusClass),
	}
	meter := sentry.NewMeter(sentry.SetHubOnContext(ctx, hub))
	meter.Count("http.server.requests", 1, sentry.WithAttributes(attrs...))
	meter.Distribution("http.server.duration", float64(duration.Microseconds())/1000, sentry.WithUnit("millisecond"), sentry.WithAttributes(attrs...))
	if status >= 500 {
		meter.Count("http.server.errors", 1, sentry.WithAttributes(attrs...))
	}
}
