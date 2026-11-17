package tasklock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestExpiredOwnerCannotExtendOrReleaseReplacement(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	first, held, err := Acquire(ctx, client, "task", time.Second)
	if err != nil || !held {
		t.Fatalf("acquire: %v", err)
	}
	if _, held, err := Acquire(ctx, client, "task", time.Second); err != nil || held {
		t.Fatal("overlapping run acquired lock")
	}
	server.FastForward(2 * time.Second)
	second, held, err := Acquire(ctx, client, "task", time.Second)
	if err != nil || !held {
		t.Fatalf("replacement: %v", err)
	}
	if held, err := first.Extend(ctx, time.Minute); err != nil || held {
		t.Fatal("expired owner extended replacement")
	}
	if held, err := first.Release(ctx); err != nil || held {
		t.Fatal("expired owner released replacement")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if held, err := second.Release(cancelled); err != nil || !held {
		t.Fatalf("release after cancel: %v", err)
	}
}

func TestHeartbeatReportsLostOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	lock, held, err := Acquire(context.Background(), client, "task", 30*time.Millisecond)
	if err != nil || !held {
		t.Fatal(err)
	}
	server.Set("task", "replacement")
	failed := make(chan error, 1)
	stop := lock.KeepAlive(context.Background(), 30*time.Millisecond, func(err error) { failed <- err })
	defer stop()
	select {
	case err := <-failed:
		if !errors.Is(err, ErrLost) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not detect replaced lock")
	}
}

// A transient Redis error must not end the heartbeat. If it did, the TTL would
// stop being refreshed while the task kept running, and a run that outlives its
// TTL would continue without exclusive ownership — letting a second replica act
// on the same rows.
//
// The observable difference is how many times the failure is reported: the loop
// keeps ticking and reporting while Redis is unreachable, whereas exiting on the
// first error reports exactly once and then goes silent forever.
func TestHeartbeatSurvivesTransientErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()

	lock, held, err := Acquire(ctx, client, "task", time.Hour)
	if err != nil || !held {
		t.Fatal(err)
	}

	// Closing the client makes every later command fail immediately and
	// identically, which is a deterministic stand-in for a Redis outage.
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	reported := make(chan error, 16)
	stop := lock.KeepAlive(ctx, 30*time.Millisecond, func(err error) { reported <- err })
	defer stop()

	deadline := time.After(3 * time.Second)
	for count := 0; count < 3; {
		select {
		case err := <-reported:
			if errors.Is(err, ErrLost) {
				t.Fatalf("a transient error was reported as ownership loss: %v", err)
			}
			count++
		case <-deadline:
			t.Fatalf("the heartbeat stopped after %d transient error(s) instead of retrying", count)
		}
	}
}

// The stop function must join the heartbeat goroutine, so a caller can stop it
// before releasing the lock without racing.
func TestKeepAliveStopIsIdempotent(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	lock, held, err := Acquire(context.Background(), client, "task", time.Hour)
	if err != nil || !held {
		t.Fatal(err)
	}
	stop := lock.KeepAlive(context.Background(), 30*time.Millisecond, nil)
	stop()
	stop()
	if held, err := lock.Release(context.Background()); err != nil || !held {
		t.Fatalf("release after stop: held=%v err=%v", held, err)
	}
}
