package node

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var publishServerCacheScript = redis.NewScript(`
if (redis.call('GET', KEYS[1]) or '0') ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
redis.call('SET', KEYS[3], ARGV[1], 'PX', ARGV[3])
return 1
`)

func ServerCacheGeneration(ctx context.Context, client *redis.Client, serverID int64) (string, error) {
	generation, err := client.Get(ctx, fmt.Sprintf(ServerCacheGenerationKey, serverID)).Result()
	if err == redis.Nil {
		return "0", nil
	}
	return generation, err
}

func PublishServerCache(ctx context.Context, client *redis.Client, serverID int64, key string, payload []byte, generation string) (bool, error) {
	result, err := publishServerCacheScript.Run(ctx, client, []string{fmt.Sprintf(ServerCacheGenerationKey, serverID), key, key + ":generation"}, generation, payload, ServerCacheTTL.Milliseconds()).Int()
	return result == 1, err
}

func ReadServerCache(ctx context.Context, client *redis.Client, key, generation string) ([]byte, time.Duration, error) {
	pipe := client.TxPipeline()
	payload := pipe.Get(ctx, key)
	version := pipe.Get(ctx, key+":generation")
	lifetime := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, 0, err
	}
	if payload.Val() == "" || (version.Val() != generation && !(version.Val() == "" && generation == "0")) {
		return nil, 0, nil
	}
	ttl := lifetime.Val()
	if ttl < 0 || ttl > ServerCacheTTL {
		ttl = ServerCacheTTL
	}
	return []byte(payload.Val()), ttl, nil
}

func InvalidateServerCaches(ctx context.Context, client *redis.Client, ids ...int64) error {
	if client == nil {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	pipe := client.TxPipeline()
	for _, serverID := range ids {
		if serverID <= 0 {
			continue
		}
		if _, exists := seen[serverID]; exists {
			continue
		}
		seen[serverID] = struct{}{}
		pipe.Incr(ctx, fmt.Sprintf(ServerCacheGenerationKey, serverID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func InvalidateServerUserListKeys(ctx context.Context, client *redis.Client, keys ...string) error {
	var ids []int64
	for _, key := range keys {
		if !strings.HasPrefix(key, ServerUserListCacheKey) {
			continue
		}
		suffix := strings.TrimPrefix(key, ServerUserListCacheKey)
		serverID, err := strconv.ParseInt(strings.SplitN(suffix, ":", 2)[0], 10, 64)
		if err == nil {
			ids = append(ids, serverID)
		}
	}
	return InvalidateServerCaches(ctx, client, ids...)
}
