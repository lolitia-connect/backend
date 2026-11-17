package node

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestDeviceLeaseConcurrentAdmissionAndGenerationFence(t *testing.T) {
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	var admitted atomic.Int64
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			decision, err := ClaimDeviceLease(ctx, client, 1, 9, fmt.Sprintf("192.0.2.%d", index+1), 1, "0")
			if err != nil {
				t.Error(err)
				return
			}
			if decision == 1 {
				admitted.Add(1)
			}
		}(index)
	}
	workers.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("concurrent grants = %d", admitted.Load())
	}
	members, err := client.ZRange(ctx, DeviceLeaseKey(9), 0, -1).Result()
	if err != nil || len(members) != 1 {
		t.Fatalf("leases = %v, %v", members, err)
	}
	if decision, err := ClaimDeviceLease(ctx, client, 2, 9, members[0], 1, "0"); err != nil || decision != 1 {
		t.Fatalf("same IP across nodes = %d, %v", decision, err)
	}
	if err := client.Set(ctx, fmt.Sprintf(ServerCacheGenerationKey, 1), "1", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if decision, err := ClaimDeviceLease(ctx, client, 1, 10, "192.0.2.100", 1, "0"); err != nil || decision != -1 {
		t.Fatalf("stale grant = %d, %v", decision, err)
	}
	if client.Exists(ctx, DeviceLeaseKey(10)).Val() != 0 {
		t.Fatal("stale generation mutated lease set")
	}
}

func TestDeviceLeaseReportsMergeAndExpire(t *testing.T) {
	cache := miniredis.RunT(t)
	now := time.Unix(1800000000, 0)
	cache.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	for _, address := range []string{"192.0.2.1", "192.0.2.2"} {
		if err := RenewDeviceLeases(ctx, client, OnlineUserSubscribe{9: {address}}); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := CountDeviceLeases(ctx, client, []int64{9, 10})
	if err != nil || counts[9] != 2 || counts[10] != 0 {
		t.Fatalf("merged counts = %v, %v", counts, err)
	}
	cache.SetTime(now.Add(Expiry))
	counts, err = CountDeviceLeases(ctx, client, []int64{9})
	if err != nil || counts[9] != 0 {
		t.Fatalf("expired counts = %v, %v", counts, err)
	}
	if decision, err := ClaimDeviceLease(ctx, client, 1, 9, "192.0.2.3", 1, "0"); err != nil || decision != 1 {
		t.Fatalf("claim after expiry = %d, %v", decision, err)
	}
}

func TestServerCacheRejectsPublicationAcrossInvalidation(t *testing.T) {
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	key := ServerUserListCacheKey + "4:vless:1"
	if ok, err := PublishServerCache(ctx, client, 4, key, []byte("old"), "0"); err != nil || !ok {
		t.Fatalf("initial publication: %v, %v", ok, err)
	}
	if err := InvalidateServerCaches(ctx, client, 4); err != nil {
		t.Fatal(err)
	}
	if ok, err := PublishServerCache(ctx, client, 4, key, []byte("stale"), "0"); err != nil || ok {
		t.Fatalf("stale publication: %v, %v", ok, err)
	}
	if data, _, err := ReadServerCache(ctx, client, key, "1"); err != nil || len(data) != 0 {
		t.Fatalf("stale cache read: %q, %v", data, err)
	}
	if ok, err := PublishServerCache(ctx, client, 4, key, []byte("new"), "1"); err != nil || !ok {
		t.Fatalf("fresh publication: %v, %v", ok, err)
	}
	if data, _, err := ReadServerCache(ctx, client, key, "1"); err != nil || string(data) != "new" {
		t.Fatalf("fresh cache read: %q, %v", data, err)
	}
}
