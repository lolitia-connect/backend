package internal

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
)

type pendingPlugin struct {
	entered chan struct{}
}

func (plugin pendingPlugin) WaitReady(ctx context.Context) error {
	close(plugin.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestStopCancelsPendingInitialization(t *testing.T) {
	plugin := pendingPlugin{entered: make(chan struct{})}
	service := NewService(&svc.ServiceContext{PluginReady: plugin})
	result := make(chan error, 1)
	go func() { result <- service.Run() }()
	<-plugin.entered
	service.Stop()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP initialization did not stop")
	}
	if err := service.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidTLSReturnsStartupError(t *testing.T) {
	_, err := newTransportServer(&svc.ServiceContext{Config: config.Config{TLS: config.TLS{Enable: true, CertFile: "missing.pem", KeyFile: "missing.key"}}}, "127.0.0.1:0")
	if err == nil {
		t.Fatal("invalid TLS files accepted")
	}
}
