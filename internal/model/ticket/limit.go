package ticket

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var takeTicketPermit = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('EXPIRE', KEYS[1], 3600) end
return count <= tonumber(ARGV[1]) and 1 or 0
`)

func AllowWrite(ctx context.Context, client *redis.Client, userID int64, reply bool) error {
	if client == nil {
		return nil
	}
	prefix, quota := "ticket:limit:user:", 5
	if reply {
		prefix, quota = "ticket:limit:follow:user:", 30
	}
	allowed, err := takeTicketPermit.Run(ctx, client, []string{prefix + strconv.FormatInt(userID, 10)}, quota).Int()
	if err != nil {
		zap.S().Errorw("ticket rate limit unavailable", "user_id", userID, "reply", reply, "error", err)
		return nil
	}
	if allowed == 0 {
		return xerr.NewErrCode(xerr.TooManyRequests)
	}
	return nil
}
