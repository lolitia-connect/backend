package node

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const ServerCacheGenerationKey = "node:server:generation:%d"

var claimDeviceLeaseScript = redis.NewScript(`
redis.replicate_commands()
if (redis.call('GET', KEYS[2]) or '0') ~= ARGV[4] then
  return -1
end
local now = tonumber(redis.call('TIME')[1])
local ttl = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local previous = redis.call('ZSCORE', KEYS[1], ARGV[1])
if previous or tonumber(ARGV[2]) <= 0 or redis.call('ZCARD', KEYS[1]) < tonumber(ARGV[2]) then
  redis.call('ZADD', KEYS[1], math.max(now + ttl, tonumber(previous) or 0), ARGV[1])
  redis.call('EXPIRE', KEYS[1], ttl)
  return 1
end
return 0
`)

var renewDeviceLeaseScript = redis.NewScript(`
redis.replicate_commands()
local now = tonumber(redis.call('TIME')[1])
local ttl = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
for index = 2, #ARGV do
  local previous = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[index])) or 0
  if previous < now + ttl then
    redis.call('ZADD', KEYS[1], now + ttl, ARGV[index])
  end
end
redis.call('EXPIRE', KEYS[1], ttl)
return 1
`)

func DeviceLeaseKey(subscriptionID int64) string {
	return fmt.Sprintf("node:online:devices:%d", subscriptionID)
}

func ClaimDeviceLease(ctx context.Context, client *redis.Client, serverID, subscriptionID int64, address string, limit int64, generation string) (int, error) {
	return claimDeviceLeaseScript.Run(ctx, client, []string{DeviceLeaseKey(subscriptionID), fmt.Sprintf(ServerCacheGenerationKey, serverID)}, address, limit, int64(Expiry.Seconds()), generation).Int()
}

func RenewDeviceLeases(ctx context.Context, client *redis.Client, online OnlineUserSubscribe) error {
	pipe := client.Pipeline()
	for subscriptionID, addresses := range online {
		if len(addresses) == 0 {
			continue
		}
		arguments := make([]any, 1, len(addresses)+1)
		arguments[0] = int64(Expiry.Seconds())
		for _, address := range addresses {
			arguments = append(arguments, address)
		}
		renewDeviceLeaseScript.Eval(ctx, pipe, []string{DeviceLeaseKey(subscriptionID)}, arguments...)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func CountDeviceLeases(ctx context.Context, client *redis.Client, ids []int64) (map[int64]int64, error) {
	counts := make(map[int64]int64, len(ids))
	now, err := client.Time(ctx).Result()
	if err != nil {
		return nil, err
	}
	for offset := 0; offset < len(ids); offset += 500 {
		end := min(offset+500, len(ids))
		pipe := client.Pipeline()
		commands := make(map[int64]*redis.IntCmd, end-offset)
		for _, subscriptionID := range ids[offset:end] {
			commands[subscriptionID] = pipe.ZCount(ctx, DeviceLeaseKey(subscriptionID), "("+strconv.FormatInt(now.Unix(), 10), "+inf")
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
		for subscriptionID, command := range commands {
			if command.Val() > 0 {
				counts[subscriptionID] = command.Val()
			}
		}
	}
	return counts, nil
}
