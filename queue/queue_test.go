package queue

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
)

func TestShutdownReturnsFromRun(t *testing.T) {
	redis := miniredis.RunT(t)
	service := NewService(&svc.ServiceContext{Config: config.Config{Redis: config.RedisConfig{Host: redis.Addr()}}})
	result := make(chan error, 1)
	go func() { result <- service.Run() }()
	service.Stop()
	service.Stop()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queue still waiting after shutdown")
	}
}
