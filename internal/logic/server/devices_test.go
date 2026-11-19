package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
)

type admissionNodes struct {
	reportNodes
	reads atomic.Int64
}

func (nodes *admissionNodes) FindOneServer(ctx context.Context, id int64) (*node.Server, error) {
	nodes.reads.Add(1)
	return nodes.reportNodes.FindOneServer(ctx, id)
}

type admissionStore struct {
	repository.Store
	nodes *admissionNodes
}

func (store admissionStore) Node() node.Model { return store.nodes }
func (store admissionStore) User() user.Model { return reportUsers{} }

func admissionFixture(t *testing.T) (*svc.ServiceContext, *admissionNodes) {
	t.Helper()
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	nodes := &admissionNodes{}
	return &svc.ServiceContext{Redis: client, Store: admissionStore{nodes: nodes}}, nodes
}

func publishAdmissionUsers(t *testing.T, service *svc.ServiceContext, instance, generation string, users ...types.ServerUser) {
	t.Helper()
	payload, err := json.Marshal(types.GetServerUserListResponse{Users: users})
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%s4:vless:%s", node.ServerUserListCacheKey, instance)
	if published, err := node.PublishServerCache(context.Background(), service.Redis, 4, key, payload, generation); err != nil || !published {
		t.Fatalf("publish = %v, %v", published, err)
	}
}

func TestDeviceClaimsShareIndexAndRespectProtocolInstance(t *testing.T) {
	service, nodes := admissionFixture(t)
	publishAdmissionUsers(t, service, "1", "0", types.ServerUser{Id: 7, UUID: "real-seven", DeviceLimit: 1})
	publishAdmissionUsers(t, service, "2", "0", types.ServerUser{Id: 8, UUID: "real-eight", DeviceLimit: 1})
	ctx := context.Background()
	devices := DeviceService{Service: service}
	request := &types.ClaimServerDeviceRequest{ServerCommon: types.ServerCommon{ServerId: 4, Protocol: "vless", ProtocolId: "1"}, SID: 7, IP: "::ffff:192.0.2.1"}
	for _, address := range []string{"::ffff:192.0.2.1", "192.0.2.1"} {
		request.IP = address
		response, err := devices.ClaimServerDevice(ctx, request)
		if err != nil || !response.Allowed || response.TTLSeconds != 300 {
			t.Fatalf("same-IP claim = %+v, %v", response, err)
		}
	}
	request.IP = "192.0.2.2"
	response, err := devices.ClaimServerDevice(ctx, request)
	if err != nil || response.Allowed {
		t.Fatalf("over-limit claim = %+v, %v", response, err)
	}
	counts, err := devices.GetServerAliveList(ctx, &types.GetServerUserListRequest{ServerCommon: request.ServerCommon})
	if err != nil || counts.Alive[7] != 1 {
		t.Fatalf("alive = %+v, %v", counts, err)
	}
	if nodes.reads.Load() != 1 {
		t.Fatalf("warm index repeated server lookup: %d", nodes.reads.Load())
	}
	request.ProtocolId = "2"
	if _, err := devices.ClaimServerDevice(ctx, request); err == nil {
		t.Fatal("cross-instance subscription admitted")
	}
	if nodes.reads.Load() != 2 {
		t.Fatalf("per-instance index lookups = %d", nodes.reads.Load())
	}
	request.ProtocolId = "1"
	for _, address := range []string{"0.0.0.0", "::", "fe80::1%eth0", "bad"} {
		request.IP = address
		if _, err := devices.ClaimServerDevice(ctx, request); err == nil {
			t.Fatalf("invalid address %q accepted", address)
		}
	}
}

type beforeLeaseHook struct {
	once       sync.Once
	invalidate func()
}

func (hook *beforeLeaseHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (hook *beforeLeaseHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (hook *beforeLeaseHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		if strings.HasPrefix(command.Name(), "eval") && len(command.Args()) > 3 && command.Args()[3] == node.DeviceLeaseKey(7) {
			hook.once.Do(hook.invalidate)
		}
		return next(ctx, command)
	}
}

func TestDeviceClaimCannotGrantAcrossRevocation(t *testing.T) {
	service, _ := admissionFixture(t)
	publishAdmissionUsers(t, service, "1", "0", types.ServerUser{Id: 7, UUID: "real-seven", DeviceLimit: 1})
	ctx := context.Background()
	if _, err := ServedSubscriptionIDs(ctx, service, 4, "vless", "1"); err != nil {
		t.Fatal(err)
	}
	service.Redis.AddHook(&beforeLeaseHook{invalidate: func() {
		if err := node.InvalidateServerCaches(ctx, service.Redis, 4); err != nil {
			t.Error(err)
		}
		publishAdmissionUsers(t, service, "1", "1", types.ServerUser{Id: 8, UUID: "real-eight", DeviceLimit: 1})
	}})
	request := &types.ClaimServerDeviceRequest{ServerCommon: types.ServerCommon{ServerId: 4, Protocol: "vless", ProtocolId: "1"}, SID: 7, IP: "192.0.2.1"}
	if _, err := (DeviceService{Service: service}).ClaimServerDevice(ctx, request); err == nil {
		t.Fatal("revoked authorization granted a device lease")
	}
	if service.Redis.Exists(ctx, node.DeviceLeaseKey(7)).Val() != 0 {
		t.Fatal("revocation race mutated device leases")
	}
}
