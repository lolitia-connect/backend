package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/transport/httpserver"
	"go.uber.org/zap"

	"github.com/perfect-panel/server/pkg/proc"
	"github.com/perfect-panel/server/pkg/trace"

	"github.com/perfect-panel/server/internal/svc"
)

type Service struct {
	server     transportServer
	svc        *svc.ServiceContext
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	restarting bool
}

func NewService(svc *svc.ServiceContext) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		svc:    svc,
		ctx:    ctx,
		cancel: cancel,
	}
}

type transportServer interface {
	Run() error
	Shutdown(ctx context.Context) error
}

func newTransportServer(svc *svc.ServiceContext, addr string) (transportServer, error) {
	var tlsConfig *tls.Config
	if svc.Config.TLS.Enable {
		cert, err := tls.LoadX509KeyPair(svc.Config.TLS.CertFile, svc.Config.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load TLS certificate: %w", err)
		}
		tlsConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
	}
	return httpserver.New(svc, addr, tlsConfig), nil
}

func (m *Service) Start() {
	if err := m.Run(); err != nil {
		panic(err)
	}
}

func (m *Service) Run() error {
	if m.ctx.Err() != nil {
		return nil
	}
	if m.svc == nil {
		return errors.New("service context is nil")
	}

	// 等待插件管理器加载完成
	if m.svc.PluginReady != nil {
		if err := m.svc.PluginReady.WaitReady(m.ctx); err != nil {
			if m.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("plugin manager not ready: %w", err)
		}
	}

	// Detect ambiguous email identities. Duplicated canonical emails make
	// login/reset lookups non-deterministic; surface the problem at boot
	// instead of failing unpredictably at request time. We log rather than
	// panic so that an existing deployment with legacy case-variant rows is
	// not taken offline by the upgrade.
	if err := m.svc.Store.User().ValidateEmailIdentityUniqueness(context.Background()); err != nil {
		zap.S().Errorf("email identity uniqueness check failed: %s", err.Error())
	}

	// get server port
	port := m.svc.Config.Port
	host := m.svc.Config.Host
	// check gateway mode
	if report.IsGatewayMode() {
		// get free port
		freePort, err := report.ModulePort()
		if err != nil {
			return err
		}
		port = freePort
		host = "127.0.0.1"
		// register module
		err = report.RegisterModule(port)
		if err != nil {
			return err
		}
		zap.S().Infof("module registered on port %d", port)
	}

	serverAddr := fmt.Sprintf("%v:%d", host, port)
	traceConfig := m.svc.Config.Trace
	if traceConfig.Name == "" {
		traceConfig.Name = trace.TraceName
	}
	trace.StartAgent(traceConfig)
	proc.AddShutdownListener(func() {
		trace.StopAgent()
	})
	defer trace.StopAgent()
	m.svc.Restart = m.Restart
	for {
		if m.ctx.Err() != nil {
			return nil
		}
		initialize.StartInitSystemConfig(m.svc)
		m.mu.Lock()
		if m.ctx.Err() != nil {
			m.mu.Unlock()
			return nil
		}
		server, err := newTransportServer(m.svc, serverAddr)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.server = server
		m.restarting = false
		m.mu.Unlock()
		zap.S().Infof("server start at %v", serverAddr)
		err = server.Run()
		m.mu.Lock()
		restart := m.restarting && m.ctx.Err() == nil
		m.mu.Unlock()
		if !restart {
			return err
		}
	}
}

func (m *Service) Stop() {
	m.cancel()
	m.mu.Lock()
	server := m.server
	m.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		zap.S().Errorf("server shutdown error: %s", err.Error())
	}
	zap.S().Info("server shutdown")
}

func (m *Service) Restart() error {
	m.mu.Lock()
	if m.server == nil || m.ctx.Err() != nil || m.restarting {
		m.mu.Unlock()
		return errors.New("server is nil")
	}
	server := m.server
	m.restarting = true
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		m.mu.Lock()
		m.restarting = false
		m.mu.Unlock()
		zap.S().Errorf("server shutdown error: %v", err.Error())
		return err
	}
	zap.S().Info("server shutdown")
	return nil
}
