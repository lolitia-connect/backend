package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/redis/go-redis/v9"
)

// oauthPendingRegisterExpire bounds how long a verified provider identity is
// held while the user is asked for an invitation code. It is kept short: the
// identity is a registration capability, not a session credential.
const oauthPendingRegisterExpire = 10 * time.Minute

// pendingOAuthIdentity is the verified provider identity held server-side
// while a forced-invite registration waits for the user to supply a code. It
// deliberately contains no persisted user record: nothing is written to the
// user tables until a valid invitation code arrives.
type pendingOAuthIdentity struct {
	Method string `json:"method"`
	OpenID string `json:"openid"`
	Email  string `json:"email"`
	Avatar string `json:"avatar"`
}

// oauthRegistrationPending signals that a brand-new OAuth identity cannot be
// registered yet because the panel still needs input from the user: an
// invitation code (invitation-only registration) and/or a real email address
// (the provider supplied none). It carries the one-time ticket the client
// echoes back when it submits the missing data, so the provider round trip
// does not have to be repeated. No user record is written while it is pending.
type oauthRegistrationPending struct {
	ticket         string
	inviteRequired bool
	emailRequired  bool
	// emailVerify reports that the collected email must be proven with a
	// verification code before the account is created.
	emailVerify bool
}

func (e *oauthRegistrationPending) Error() string { return "oauth registration pending user input" }

func pendingOAuthKey(ticket string) string {
	return fmt.Sprintf("%s:%s", config.OAuthPendingRegisterKey, ticket)
}

// cachePendingOAuth stores a verified provider identity under a fresh one-time
// ticket and returns that ticket.
func cachePendingOAuth(ctx context.Context, rdb *redis.Client, identity *pendingOAuthIdentity) (string, error) {
	ticket := uuidx.NewUUID().String()
	payload, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	if err := rdb.Set(ctx, pendingOAuthKey(ticket), string(payload), oauthPendingRegisterExpire).Err(); err != nil {
		return "", err
	}
	return ticket, nil
}

// loadPendingOAuth reads a previously cached provider identity by ticket. A
// missing or expired ticket surfaces as an error.
func loadPendingOAuth(ctx context.Context, rdb *redis.Client, ticket string) (*pendingOAuthIdentity, error) {
	if ticket == "" {
		return nil, redis.Nil
	}
	payload, err := rdb.Get(ctx, pendingOAuthKey(ticket)).Result()
	if err != nil {
		return nil, err
	}
	var identity pendingOAuthIdentity
	if err := json.Unmarshal([]byte(payload), &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

// consumePendingOAuth removes a ticket so it cannot be reused.
func consumePendingOAuth(ctx context.Context, rdb *redis.Client, ticket string) {
	if ticket == "" {
		return
	}
	_ = rdb.Del(ctx, pendingOAuthKey(ticket)).Err()
}
