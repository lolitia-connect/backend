package usersession

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func VerifyPassword(ctx context.Context, client *redis.Client, userID int64, algorithm, salt, password, encoded string) error {
	if client == nil || password == "" || encoded == "" {
		return xerr.NewErrCode(xerr.UserPasswordError)
	}
	key := "auth:password_attempts:" + strconv.FormatInt(userID, 10)
	count, err := client.Eval(ctx, `local n = redis.call('INCR', KEYS[1]); if n == 1 then redis.call('EXPIRE', KEYS[1], 900) end; return n`, []string{key}).Int64()
	if err != nil {
		return err
	}
	if count > 10 {
		return xerr.NewErrCode(xerr.TooManyRequests)
	}
	if !tool.MultiPasswordVerify(algorithm, salt, password, encoded) {
		return xerr.NewErrCode(xerr.UserPasswordError)
	}
	return client.Eval(ctx, `if tonumber(redis.call('GET', KEYS[1]) or '0') > 0 then return redis.call('DECR', KEYS[1]) end; return 0`, []string{key}).Err()
}

func verifiedCode(ctx context.Context, client *redis.Client, key, code string) (string, error) {
	invalid := xerr.NewErrCode(xerr.VerifyCodeError)
	if code == "" || client == nil {
		return "", invalid
	}
	value, err := client.Get(ctx, key).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			zap.L().Error("Verification code lookup failed", zap.Error(err))
		}
		return "", invalid
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		zap.L().Error("Invalid cached verification record", zap.Error(err))
		return "", invalid
	}
	if subtle.ConstantTimeCompare([]byte(payload.Code), []byte(code)) != 1 {
		return "", invalid
	}
	return value, nil
}

func VerifyCode(ctx context.Context, client *redis.Client, key, code string) error {
	_, err := verifiedCode(ctx, client, key, code)
	return err
}

func ConsumeCode(ctx context.Context, client *redis.Client, key, code string) error {
	value, err := verifiedCode(ctx, client, key, code)
	if err != nil {
		return err
	}
	removed, err := client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end; return 0`, []string{key}, value).Int64()
	if err != nil {
		zap.L().Error("Verification code consumption failed", zap.Error(err))
	}
	if err != nil || removed != 1 {
		return xerr.NewErrCode(xerr.VerifyCodeError)
	}
	return nil
}
