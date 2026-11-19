package usersession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/redis/go-redis/v9"
)

func TestSessionRevocationAndConcurrentIssue(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	epoch, err := AcquireEpoch(ctx, client, 7)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Issue(ctx, client, "secret", 3600, 7, "session", "email", epoch, "")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.ParseJwtToken(signed, "secret")
	if err != nil || Check(ctx, client, 7, claims) != nil {
		t.Fatalf("issued token is invalid: %v", err)
	}
	if err := Check(ctx, client, 7, map[string]any{}); err != nil {
		t.Fatal("legacy token rejected before revocation")
	}
	before := time.Now().Add(-time.Second)
	if _, err := Rotate(ctx, client, 7); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(Check(ctx, client, 7, claims), ErrRevoked) || !errors.Is(Check(ctx, client, 7, map[string]any{}), ErrRevoked) {
		t.Fatal("revocation accepted an old or legacy session")
	}
	if _, err := Issue(ctx, client, "secret", 3600, 7, "racing", "email", epoch, ""); !errors.Is(err, ErrRevoked) {
		t.Fatalf("concurrent sign-in was not fenced: %v", err)
	}
	if revoked, err := RevokedSince(ctx, client, 7, before); err != nil || !revoked {
		t.Fatalf("checkout capability revocation = %v, %v", revoked, err)
	}
	server.Del(Key(7))
	if !errors.Is(Check(ctx, client, 7, claims), ErrRevoked) {
		t.Fatal("missing epoch accepted an issued session")
	}
}

func TestCodeIsConsumedOnlyAfterProofAndOnlyOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	server.Set("code", `{"code":"123456"}`)
	if ConsumeCode(ctx, client, "code", "wrong") == nil || !server.Exists("code") {
		t.Fatal("wrong code accepted or consumed the real code")
	}
	if err := ConsumeCode(ctx, client, "code", "123456"); err != nil {
		t.Fatal(err)
	}
	if ConsumeCode(ctx, client, "code", "123456") == nil {
		t.Fatal("verification code replay accepted")
	}
}

func TestPasswordAttemptsHaveAnAtomicWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	for attempt := 0; attempt < 10; attempt++ {
		_ = VerifyPassword(ctx, client, 7, "md5", "", "wrong", "098f6bcd4621d373cade4e832627b4f6")
	}
	if err := VerifyPassword(ctx, client, 7, "md5", "", "test", "098f6bcd4621d373cade4e832627b4f6"); err == nil {
		t.Fatal("eleventh password attempt was accepted")
	}
	server.FastForward(16 * time.Minute)
	if err := VerifyPassword(ctx, client, 7, "md5", "", "test", "098f6bcd4621d373cade4e832627b4f6"); err != nil {
		t.Fatal(err)
	}
}
