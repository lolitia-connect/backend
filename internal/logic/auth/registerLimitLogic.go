package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func registerIpLimit(svcCtx *svc.ServiceContext, ctx context.Context, registerIp, authType, account string) (isOk bool) {
	if !svcCtx.Config.Register.EnableIpRegisterLimit {
		return true
	}

	// Use a sorted set to track IP registrations with timestamp as score
	// Key format: register:ip:{ip}
	key := fmt.Sprintf("%s%s", config.RegisterIpKeyPrefix, registerIp)
	now := time.Now().Unix()
	expiration := int64(svcCtx.Config.Register.IpRegisterLimitDuration) * 60

	// Clean up expired entries first (remove entries older than expiration duration)
	expireTimestamp := now - expiration
	removed, err := svcCtx.Redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", expireTimestamp)).Result()
	if err != nil {
		zap.S().Errorf("[registerIpLimit] ZRemRangeByScore Err: %v", err)
		return true
	}
	if removed > 0 {
		zap.S().Debugf("[registerIpLimit] Cleaned %d expired entries for IP: %s", removed, registerIp)
	}

	// Get current count of registrations within the time window
	count, err := svcCtx.Redis.ZCard(ctx, key).Result()
	if err != nil {
		zap.S().Errorf("[registerIpLimit] ZCard Err: %v", err)
		return true
	}

	// Check if limit is reached
	if count >= svcCtx.Config.Register.IpRegisterLimit {
		zap.S().Warnf("[registerIpLimit] IP %s exceeded limit: %d/%d", registerIp, count, svcCtx.Config.Register.IpRegisterLimit)
		return false
	}

	// Add new registration entry with current timestamp as score
	member := fmt.Sprintf("%s:%s", authType, account)
	if err := svcCtx.Redis.ZAdd(ctx, key, redis.Z{
		Score:  float64(now),
		Member: member,
	}).Err(); err != nil {
		zap.S().Errorf("[registerIpLimit] ZAdd Err: %v", err)
		return true
	}

	// Set expiration on the sorted set key
	if err := svcCtx.Redis.Expire(ctx, key, time.Minute*time.Duration(svcCtx.Config.Register.IpRegisterLimitDuration)).Err(); err != nil {
		zap.S().Errorf("[registerIpLimit] Expire Err: %v", err)
	}

	return true
}
