package trace

import (
	"context"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func TestStartAgent(t *testing.T) {
	zap.ReplaceGlobals(zap.NewNop())

	const (
		endpoint1 = "localhost:1234"
		endpoint2 = "remotehost:1234"
		endpoint3 = "localhost:1235"
		endpoint4 = "localhost:1236"
		endpoint5 = "udp://localhost:6831"
		endpoint6 = "localhost:1237"
	)
	endpoint71 := filepath.Join(t.TempDir(), "trace.log")
	endpoint72 := filepath.Join(t.TempDir(), "missing", "trace.log")
	c1 := Config{
		Name: "foo",
	}
	c2 := Config{
		Name:     "bar",
		Endpoint: endpoint1,
		Batcher:  kindJaeger,
	}
	c3 := Config{
		Name:     "any",
		Endpoint: endpoint2,
		Batcher:  kindZipkin,
	}
	c4 := Config{
		Name:     "bla",
		Endpoint: endpoint3,
		Batcher:  "otlp",
	}
	c5 := Config{
		Name:     "otlpgrpc",
		Endpoint: endpoint3,
		Batcher:  kindOtlpGrpc,
		OtlpHeaders: map[string]string{
			"uptrace-dsn": "http://project2_secret_token@localhost:14317/2",
		},
	}
	c6 := Config{
		Name:     "otlphttp",
		Endpoint: endpoint4,
		Batcher:  kindOtlpHttp,
		OtlpHeaders: map[string]string{
			"uptrace-dsn": "http://project2_secret_token@localhost:14318/2",
		},
		OtlpHttpPath: "/v1/traces",
	}
	c7 := Config{
		Name:     "UDP",
		Endpoint: endpoint5,
		Batcher:  kindJaeger,
	}
	c8 := Config{
		Disabled: true,
		Endpoint: endpoint6,
		Batcher:  kindJaeger,
	}
	c10 := Config{
		Name:     "file",
		Endpoint: endpoint72,
		Batcher:  kindFile,
	}

	StartAgent(c1)
	StartAgent(c1)
	StartAgent(c2)
	StartAgent(c3)
	StartAgent(c4)
	StartAgent(c5)
	StartAgent(c6)
	StartAgent(c7)
	StartAgent(c8)
	StartAgent(c10)
	defer StopAgent()

	lock.Lock()
	defer lock.Unlock()

	// because remotehost cannot be resolved
	assert.Equal(t, 5, len(agents))
	_, ok := agents[""]
	assert.True(t, ok)
	_, ok = agents[endpoint1]
	assert.True(t, ok)
	_, ok = agents[endpoint2]
	assert.False(t, ok)
	_, ok = agents[endpoint5]
	assert.True(t, ok)
	_, ok = agents[endpoint6]
	assert.False(t, ok)
	_, ok = agents[endpoint72]
	assert.False(t, ok)
	_ = endpoint71
}

type traceCollector struct {
	collector.UnimplementedTraceServiceServer
	received atomic.Int32
}

func (server *traceCollector) Export(context.Context, *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	server.received.Add(1)
	return &collector.ExportTraceServiceResponse{}, nil
}

func TestGrpcExporterTLSDoesNotDowngrade(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	receiver := &traceCollector{}
	collector.RegisterTraceServiceServer(server, receiver)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	span := tracetest.SpanStub{Name: "probe"}.Snapshot()
	for _, secure := range []bool{false, true} {
		exporter, err := createExporter(Config{Batcher: kindOtlpGrpc, Endpoint: listener.Addr().String(), OtlpGrpcSecure: secure})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{span})
		cancel()
		_ = exporter.Shutdown(context.Background())
		if (err != nil) != secure {
			t.Fatalf("secure=%t export error=%v", secure, err)
		}
	}
	if receiver.received.Load() != 1 {
		t.Fatal("TLS exporter sent plaintext or default exporter did not deliver")
	}
}
