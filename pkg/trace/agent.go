package trace

//nolint:staticcheck
import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/perfect-panel/server/pkg/lang"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.uber.org/zap"
)

const (
	kindJaeger   = "jaeger"
	kindZipkin   = "zipkin"
	kindOtlpGrpc = "otlpgrpc"
	kindOtlpHttp = "otlphttp"
	kindFile     = "file"
	protocolUdp  = "udp"
)

var (
	agents = make(map[string]lang.PlaceholderType)
	lock   sync.Mutex
	tp     *sdktrace.TracerProvider
)

// StartAgent starts an opentelemetry agent.
func StartAgent(c Config) {
	if c.Disabled {
		return
	}
	zap.S().Info("Starting agent")
	lock.Lock()
	defer lock.Unlock()

	_, ok := agents[c.Endpoint]
	if ok {
		return
	}

	// if error happens, let later calls run.
	if err := startAgent(c); err != nil {
		return
	}

	agents[c.Endpoint] = lang.Placeholder
}

// StopAgent shuts down the span processors in the order they were registered.
func StopAgent() {
	lock.Lock()
	defer lock.Unlock()

	if tp != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
		tp = nil
	}
	clear(agents)
}

func createExporter(c Config) (sdktrace.SpanExporter, error) {
	// Just support jaeger and zipkin now, more for later
	switch c.Batcher {
	case kindJaeger:
		return otlptracehttp.New(context.Background(), jaegerOptions(c)...)
	case kindZipkin:
		zap.S().Warn("zipkin trace exporter is deprecated; use OTLP")
		return zipkin.New(c.Endpoint)
	case kindOtlpGrpc:
		// Always treat trace exporter as optional component, so we use nonblock here,
		// otherwise this would slow down app start up even set a dial timeout here when
		// endpoint can not reach.
		// If the connection not dial success, the global otel ErrorHandler will catch error
		// when reporting data like other exporters.
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(c.Endpoint),
		}
		if !c.OtlpGrpcSecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(c.OtlpHeaders))
		}
		return otlptracegrpc.New(context.Background(), opts...)
	case kindOtlpHttp:
		// Not support flexible configuration now.
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(c.Endpoint),
		}

		if !c.OtlpHttpSecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(c.OtlpHeaders))
		}
		if len(c.OtlpHttpPath) > 0 {
			opts = append(opts, otlptracehttp.WithURLPath(c.OtlpHttpPath))
		}
		return otlptracehttp.New(context.Background(), opts...)
	case kindFile:
		f, err := os.OpenFile(c.Endpoint, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return nil, fmt.Errorf("file exporter endpoint error: %s", err.Error())
		}
		return stdouttrace.New(stdouttrace.WithWriter(f))
	default:
		return nil, fmt.Errorf("unknown exporter: %s", c.Batcher)
	}
}

func jaegerOptions(config Config) []otlptracehttp.Option {
	options := []otlptracehttp.Option{}
	endpoint, err := url.Parse(config.Endpoint)
	if err == nil && (endpoint.Scheme == protocolUdp || endpoint.Scheme == "http" || endpoint.Scheme == "https") {
		if endpoint.Scheme == protocolUdp || endpoint.Path == "/api/traces" {
			if endpoint.Scheme == protocolUdp || endpoint.Port() == "" || endpoint.Port() == "14268" {
				endpoint.Host = net.JoinHostPort(endpoint.Hostname(), "4318")
			}
			if endpoint.Scheme == protocolUdp {
				endpoint.Scheme = "http"
			}
			endpoint.Path = "/v1/traces"
			zap.S().Warn("legacy Jaeger endpoint uses OTLP HTTP on port 4318")
		} else if endpoint.Path == "" || endpoint.Path == "/" {
			endpoint.Path = "/v1/traces"
		}
		options = append(options, otlptracehttp.WithEndpointURL(endpoint.String()))
	} else {
		options = append(options, otlptracehttp.WithEndpoint(config.Endpoint))
		if !config.OtlpHttpSecure {
			options = append(options, otlptracehttp.WithInsecure())
		}
		if config.OtlpHttpPath != "" {
			options = append(options, otlptracehttp.WithURLPath(config.OtlpHttpPath))
		}
	}
	if len(config.OtlpHeaders) > 0 {
		options = append(options, otlptracehttp.WithHeaders(config.OtlpHeaders))
	}
	return options
}

func startAgent(c Config) error {
	AddResources(semconv.ServiceNameKey.String(c.Name))

	opts := []sdktrace.TracerProviderOption{
		// Set the sampling rate based on the parent span to 100%
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Sampler))),
		// Record information about this application in a Resource.
		sdktrace.WithResource(resource.NewSchemaless(attrResources...)),
	}

	if len(c.Endpoint) > 0 {
		exp, err := createExporter(c)
		if err != nil {
			zap.S().Error(err)
			return err
		}

		// Always be sure to batch in production.
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp = sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		zap.S().Errorf("[otel] error: %v", err)
	}))

	return nil
}
