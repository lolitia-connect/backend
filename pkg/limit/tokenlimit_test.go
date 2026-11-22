package limit

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func TestTokenLimit_WithCtx(t *testing.T) {
	const (
		total = 100
		rate  = 5
		burst = 10
	)
	store, _ := CreateRedisWithClean(t)
	l := NewTokenLimiter(rate, burst, store, "tokenlimit")

	ctx, cancel := context.WithCancel(context.Background())
	ok := l.AllowCtx(ctx)
	assert.True(t, ok)

	cancel()
	for i := 0; i < total; i++ {
		ok := l.AllowCtx(ctx)
		assert.False(t, ok)
		assert.False(t, l.monitorStarted)
	}
}

func TestTokenLimit_RefillsAtConfiguredRateAndCapsAtBurst(t *testing.T) {
	store, _ := CreateRedisWithClean(t)
	const rate, burst = 5, 10
	limiter := NewTokenLimiter(rate, burst, store, "tokenlimit")
	start := time.Unix(1_000, 0)

	assert.True(t, limiter.AllowN(start, burst), "a new bucket permits a full burst")
	assert.False(t, limiter.AllowN(start, 1), "an empty bucket rejects even one token")
	assert.False(t, limiter.AllowN(start.Add(999*time.Millisecond), 1), "no refill before the next second")

	refill := start.Add(time.Second)
	assert.False(t, limiter.AllowN(refill, rate+1), "a one-second refill cannot exceed the configured rate")
	assert.True(t, limiter.AllowN(refill, rate), "rejecting a request leaves the refilled tokens available")
	assert.False(t, limiter.AllowN(refill, 1), "the refill is exhausted")

	afterIdle := start.Add(10 * time.Second)
	assert.False(t, limiter.AllowN(afterIdle, burst+1), "idle time cannot grow the bucket past the burst cap")
	assert.True(t, limiter.AllowN(afterIdle, burst), "an idle bucket permits a full burst again")
}

// CreateRedisWithClean returns an in process redis.Redis and a clean function.
func CreateRedisWithClean(t *testing.T) (r *redis.Client, clean func()) {
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	}), mr.Close
}
