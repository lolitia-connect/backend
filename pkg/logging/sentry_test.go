package logging

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/getsentry/sentry-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSentryReportsOnlySanitizedFailures(t *testing.T) {
	events := make(chan []byte, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/42/envelope/" {
			t.Errorf("unexpected project endpoint %s", r.URL.Path)
		}
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(reader)
			if err != nil {
				t.Error(err)
				return
			}
			defer gz.Close()
			reader = gz
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Error(err)
			return
		}
		events <- body
	}))
	defer server.Close()
	dsn := strings.Replace(server.URL, "://", "://sdk-key@", 1) + "/42"
	defer CloseSentry()
	local, logs := observer.New(zapcore.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(zapcore.NewTee(local, &backendSentry)))
	defer restore()
	logger := zap.L().With(zap.String("request_body", "private-field"))
	logger.Error("disabled-first")
	if err := ConfigureSentry(true, dsn, true); err != nil {
		t.Fatal(err)
	}
	previousHertzLogger := hlog.DefaultLogger()
	hlog.SetLogger(NewHertzLogger())
	defer hlog.SetLogger(previousHertzLogger)
	h := hlog.SystemLogger()
	h.Errorf(hlog.EngineErrorFormat, "error when reading request headers: malformed HTTP request. Buffer size=12, contents: \"SSH-2.0-Go\\r\\n\"", "untrusted-peer")
	logger.Warn("expected denial")
	h.Errorf(hlog.EngineErrorFormat, "error when reading request headers: unexpected reader failure. Buffer size=1, contents: private-payload", "untrusted-peer")
	logger.Error("private-message", zap.String("token", "private-token"))
	if err := ConfigureSentry(false, "", false); err != nil {
		t.Fatal(err)
	}
	logger.Error("disabled-again")
	if len(events) != 3 {
		t.Fatalf("delivered %d envelopes, want two error events and one log envelope", len(events))
	}
	errorEvents := 0
	logEnvelopes := 0
	for range 3 {
		envelope := <-events
		lines := bytes.Split(bytes.TrimSpace(envelope), []byte("\n"))
		if len(lines) < 3 {
			t.Fatal("missing envelope item")
		}
		payload := lines[2]
		if bytes.Contains(lines[1], []byte(`"type":"log"`)) {
			logEnvelopes++
			if bytes.Contains(payload, []byte("private-token")) {
				t.Fatal("sensitive log attribute was not redacted")
			}
			continue
		}
		for _, secret := range []string{"private-", "SSH-2.0", "untrusted-peer", "sdk-key", "request_body", `"server_name"`, `"abs_path"`} {
			if bytes.Contains(payload, []byte(secret)) {
				t.Fatalf("unexpected %q in event", secret)
			}
		}
		if !bytes.Contains(lines[1], []byte(`"type":"event"`)) {
			t.Fatalf("unexpected envelope header: %s", lines[1])
		}
		errorEvents++
		var event sentry.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Level != sentry.LevelError || event.Environment != "development" || event.Release == "" || len(event.Exception) == 0 || event.Exception[0].Stacktrace == nil || len(event.Exception[0].Stacktrace.Frames) == 0 {
			t.Fatalf("incomplete event: %s", payload)
		}
	}
	if errorEvents != 2 || logEnvelopes != 1 {
		t.Fatalf("got %d error events and %d log envelopes, want 2 and 1", errorEvents, logEnvelopes)
	}
	if logs.FilterLevelExact(zapcore.DebugLevel).Len() != 1 || logs.FilterLevelExact(zapcore.ErrorLevel).Len() != 4 {
		t.Fatal("unexpected local severity for malformed traffic or genuine failures")
	}
}
