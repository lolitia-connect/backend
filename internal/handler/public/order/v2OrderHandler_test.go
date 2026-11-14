package order

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/redis/go-redis/v9"
)

func TestValidIdempotencyKeyAcceptsOnlyOpaquePrintableKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "uuid with dashes", key: "0f8fad5b-d9cb-469f-a165-70867728950e", want: true},
		{name: "hex", key: "0123456789abcdef0123456789abcdef", want: true},
		{name: "shortest allowed", key: "0123456789abcdef", want: true},
		{name: "too short", key: "0123456789abcde", want: false},
		{name: "too long", key: strings.Repeat("a", 129), want: false},
		{name: "empty", key: "", want: false},
		{name: "contains a space", key: "0123456789abcde ", want: false},
		{name: "contains a newline", key: "0123456789abcde\n", want: false},
		{name: "leading whitespace", key: " 123456789abcdef", want: false},
		{name: "non ascii", key: "0123456789abcde\u00e9", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validIdempotencyKey(tt.key); got != tt.want {
				t.Fatalf("validIdempotencyKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func newV2EventStreamContext(t *testing.T, lastEventID, after string) *hertzx.Context {
	t.Helper()
	uri := "/v2/public/orders/ORDER/events"
	if after != "" {
		uri += "?after=" + after
	}
	raw := app.NewContext(0)
	raw.Request.SetRequestURI(uri)
	if lastEventID != "" {
		raw.Request.Header.Set("Last-Event-ID", lastEventID)
	}
	return hertzx.NewContext(context.Background(), raw)
}

// Last-Event-ID is managed by the browser on reconnect and must win over an
// explicit cursor; both must degrade to a full replay instead of an error.
func TestRequestedEventIDPrefersTheHeaderAndFallsBackToTheCursor(t *testing.T) {
	tests := []struct {
		name        string
		lastEventID string
		after       string
		want        int64
	}{
		{name: "no cursor at all", want: 0},
		{name: "header", lastEventID: "42", want: 42},
		{name: "query cursor", after: "17", want: 17},
		{name: "header wins", lastEventID: "42", after: "17", want: 42},
		{name: "unparseable header falls back to zero", lastEventID: "not-a-number", want: 0},
		{name: "negative cursor is treated as a full replay", lastEventID: "-3", want: 0},
		{name: "padded header", lastEventID: " 9 ", want: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newV2EventStreamContext(t, tt.lastEventID, tt.after)
			if got := requestedEventID(ctx); got != tt.want {
				t.Fatalf("requestedEventID() = %d, want %d", got, tt.want)
			}
		})
	}
}

func newV2TestRedis(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: server.Addr()})
}

func TestAcquireSSEConnectionCapsStreamsPerTicket(t *testing.T) {
	client := newV2TestRedis(t)
	ctx := context.Background()
	ticket := "stream-ticket"

	releases := make([]func(), 0, v2SSEMaxConnectionsPerTicket)
	for i := 0; i < v2SSEMaxConnectionsPerTicket; i++ {
		release, allowed := acquireSSEConnection(ctx, client, ticket, time.Minute)
		if !allowed {
			t.Fatalf("connection %d was refused below the cap", i+1)
		}
		releases = append(releases, release)
	}
	if _, allowed := acquireSSEConnection(ctx, client, ticket, time.Minute); allowed {
		t.Fatalf("connection %d was allowed above the cap of %d", v2SSEMaxConnectionsPerTicket+1, v2SSEMaxConnectionsPerTicket)
	}

	// A refusal must not consume a slot, otherwise a rejected client would
	// permanently reduce the budget of the ticket.
	releases[0]()
	if _, allowed := acquireSSEConnection(ctx, client, ticket, time.Minute); !allowed {
		t.Fatal("a released slot was not reusable")
	}

	// A different ticket is a different budget.
	if _, allowed := acquireSSEConnection(ctx, client, "another-ticket", time.Minute); !allowed {
		t.Fatal("an unrelated ticket shared the connection budget")
	}
}

func TestAcquireSSEConnectionServesStreamsWhenRedisIsUnavailable(t *testing.T) {
	release, allowed := acquireSSEConnection(context.Background(), nil, "stream-ticket", time.Minute)
	if !allowed {
		t.Fatal("a nil redis client refused the connection")
	}
	release()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	server.Close()
	release, allowed = acquireSSEConnection(context.Background(), client, "stream-ticket", time.Minute)
	if !allowed {
		t.Fatal("a broken redis client refused the connection")
	}
	release()
}
