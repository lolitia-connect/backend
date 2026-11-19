package usersession

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strconv"

	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
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

func ConsumeCode(ctx context.Context, client *redis.Client, key, code string) error {
	if code == "" || client == nil {
		return xerr.NewErrCode(xerr.VerifyCodeError)
	}
	value, err := client.Get(ctx, key).Result()
	if err != nil {
		return xerr.NewErrCode(xerr.VerifyCodeError)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(value), &payload) != nil || payload.Code == "" || subtle.ConstantTimeCompare([]byte(payload.Code), []byte(code)) != 1 {
		return xerr.NewErrCode(xerr.VerifyCodeError)
	}
	removed, err := client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end; return 0`, []string{key}, value).Int64()
	if err != nil || removed != 1 {
		return xerr.NewErrCode(xerr.VerifyCodeError)
	}
	return nil
}
