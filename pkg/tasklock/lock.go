package tasklock

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrLost = errors.New("task lock ownership lost")

type Lock struct {
	client *redis.Client
	key    string
	token  string
}

func Acquire(ctx context.Context, client *redis.Client, key string, ttl time.Duration) (*Lock, bool, error) {
	if client == nil || key == "" || ttl < time.Millisecond {
		return nil, false, errors.New("invalid task lock configuration")
	}
	token := uuid.NewString()
	held, err := client.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !held {
		return nil, false, err
	}
	return &Lock{client: client, key: key, token: token}, true, nil
}

func (lock *Lock) Extend(ctx context.Context, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, errors.New("invalid task lock duration")
	}
	count, err := lock.client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('PEXPIRE', KEYS[1], ARGV[2]) end return 0`, []string{lock.key}, lock.token, ttl.Milliseconds()).Int()
	return count == 1, err
}

func (lock *Lock) Release(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	count, err := lock.client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0`, []string{lock.key}, lock.token).Int()
	return count == 1, err
}

// KeepAlive refreshes the lock's TTL until the returned stop function is
// called, the context is cancelled, or the lock is lost.
//
// A transient Redis error does NOT stop the heartbeat: an outage of a few
// hundred milliseconds would otherwise end the refreshes, the TTL would lapse,
// and a run that outlives it would keep working without exclusive ownership —
// two replicas could then act on the same rows. Upstream 4981b2e6 makes the
// same distinction. Only a definite "we no longer own the key" stops the loop.
func (lock *Lock) KeepAlive(ctx context.Context, ttl time.Duration, failed func(error)) func() {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		interval := ttl / 3
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				held, err := lock.Extend(ctx, ttl)
				if err != nil {
					// The next tick retries; the lock is still ours until its
					// TTL actually lapses.
					if failed != nil {
						failed(err)
					}
					continue
				}
				if !held {
					if failed != nil {
						failed(ErrLost)
					}
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-stopped }) }
}
