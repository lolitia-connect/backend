package httpserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/server/internal/svc"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
)

type healthProbe struct {
	name string
	ping func(context.Context) error
}

type healthChecker struct {
	mutex     sync.Mutex
	checkedAt time.Time
	reason    string
	probes    []healthProbe
}

func registerHealthHandlers(engine *server.Hertz, service *svc.ServiceContext) {
	checker := &healthChecker{}
	if service.SQLDB != nil {
		checker.probes = append(checker.probes, healthProbe{name: "database", ping: service.SQLDB.PingContext})
	}
	if service.Redis != nil {
		checker.probes = append(checker.probes, healthProbe{name: "redis", ping: func(ctx context.Context) error {
			return service.Redis.Ping(ctx).Err()
		}})
	}
	engine.GET(HealthzPath, func(_ context.Context, request *app.RequestContext) {
		request.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	engine.GET(ReadyzPath, func(ctx context.Context, request *app.RequestContext) {
		reason := ""
		switch {
		case service.SQLDB == nil:
			reason = "database unavailable"
		case service.Redis == nil:
			reason = "redis unavailable"
		default:
			reason = checker.notReady(ctx)
		}
		if reason != "" {
			request.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "reason": reason})
			return
		}
		request.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
}

func (checker *healthChecker) notReady(ctx context.Context) string {
	checker.mutex.Lock()
	defer checker.mutex.Unlock()
	if !checker.checkedAt.IsZero() && time.Since(checker.checkedAt) < 5*time.Second {
		return checker.reason
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	checker.reason = ""
	for _, probe := range checker.probes {
		if err := probe.ping(ctx); err != nil {
			checker.reason = probe.name + " unreachable"
			break
		}
	}
	checker.checkedAt = time.Now()
	return checker.reason
}
