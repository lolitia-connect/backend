package httpserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/perfect-panel/server/internal/handler"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"go.uber.org/zap"
)

type Server struct {
	h            *server.Hertz
	transport    *listenerTransport
	shuttingDown atomic.Bool
}

type listenerTransport struct {
	mu       sync.Mutex
	options  config.Options
	delegate network.Transporter
	listener net.Listener
	stopped  bool
}

func (transport *listenerTransport) ListenAndServe(onData network.OnData) error {
	transport.mu.Lock()
	if transport.stopped {
		transport.mu.Unlock()
		return nil
	}
	listener := transport.options.Listener
	if listener == nil {
		listenConfig := transport.options.ListenConfig
		if listenConfig == nil {
			listenConfig = &net.ListenConfig{}
		}
		var err error
		listener, err = listenConfig.Listen(context.Background(), transport.options.Network, transport.options.Addr)
		if err != nil {
			transport.mu.Unlock()
			return err
		}
	}
	transport.listener = listener
	transport.options.Listener = listener
	transport.delegate = platformTransport(&transport.options)
	delegate := transport.delegate
	transport.mu.Unlock()
	defer listener.Close()
	return delegate.ListenAndServe(onData)
}

func (transport *listenerTransport) Shutdown(ctx context.Context) error {
	transport.mu.Lock()
	transport.stopped = true
	delegate, listener := transport.delegate, transport.listener
	transport.mu.Unlock()
	if listener != nil {
		defer listener.Close()
	}
	if delegate != nil {
		return delegate.Shutdown(ctx)
	}
	return nil
}

func (transport *listenerTransport) Close() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return transport.Shutdown(ctx)
}

func New(svc *svc.ServiceContext, addr string, tlsConfig *tls.Config) *Server {
	opts := []config.Option{
		server.WithHostPorts(addr),
		server.WithDisablePrintRoute(true),
	}
	if tlsConfig != nil {
		opts = append(opts, server.WithTLS(tlsConfig))
	}
	if svc.Config.HTTP.ReadTimeoutSeconds > 0 {
		opts = append(opts, server.WithReadTimeout(time.Duration(svc.Config.HTTP.ReadTimeoutSeconds)*time.Second))
	}
	if svc.Config.HTTP.WriteTimeoutSeconds > 0 {
		opts = append(opts, server.WithWriteTimeout(time.Duration(svc.Config.HTTP.WriteTimeoutSeconds)*time.Second))
	}
	if svc.Config.HTTP.IdleTimeoutSeconds > 0 {
		opts = append(opts, server.WithIdleTimeout(time.Duration(svc.Config.HTTP.IdleTimeoutSeconds)*time.Second))
	}
	if svc.Config.HTTP.MaxRequestBodyMB > 0 {
		opts = append(opts, server.WithMaxRequestBodySize(svc.Config.HTTP.MaxRequestBodyMB<<20))
	}

	return newServer(svc, opts)
}

func newServer(svc *svc.ServiceContext, opts []config.Option) *Server {
	transport := &listenerTransport{}
	opts = append(opts, server.WithTransport(func(options *config.Options) network.Transporter {
		transport.options = *options
		return transport
	}))
	engine := hertzx.Default(opts...)
	trusted, err := ParseTrustedProxies(svc.Config.TrustedProxies)
	if err != nil {
		zap.S().Errorf("trusted proxies configuration: %v; forwarding headers disabled", err)
	}
	resolveClientIP := clientIPFunc(trusted)
	engine.Hertz().SetClientIPFunc(resolveClientIP)
	engine.Hertz().Use(func(ctx context.Context, request *app.RequestContext) {
		request.SetClientIPFunc(resolveClientIP)
		request.Next(ctx)
	}, middleware.TraceMiddleware(svc), middleware.EntCacheMiddleware(), middleware.LoggerMiddleware(svc), middleware.NewCorsMiddleware(svc.Config.AllowedOrigins))

	registerHealthHandlers(engine.Hertz(), svc)
	handler.RegisterNativeHandlers(engine.Hertz(), svc)
	handler.RegisterHandlers(engine, svc)
	handler.RegisterV2OrderHandlers(engine.Hertz(), svc)
	handler.RegisterTelegramHandlers(engine, svc)
	handler.RegisterNotifyHandlers(engine, svc)

	// 注册插件路由
	if mgr, ok := svc.PluginMgr.(*plugin.Manager); ok {
		handler.RegisterPluginHandlers(engine, svc, mgr)
	}

	return &Server{h: engine.Hertz(), transport: transport}
}

func (s *Server) Start() {
	if err := s.Run(); err != nil {
		panic(err)
	}
}

func (s *Server) Run() (err error) {
	if s.shuttingDown.Load() {
		return nil
	}
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("HTTP listener failed: %v", failure)
		}
		if s.shuttingDown.Load() {
			err = nil
		}
	}()
	err = s.h.Run()
	if s.shuttingDown.Load() {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.shuttingDown.Store(true)
	if s.h.IsRunning() {
		return s.h.Shutdown(ctx)
	}
	return s.transport.Shutdown(ctx)
}

func (s *Server) Engine() *server.Hertz {
	return s.h
}
