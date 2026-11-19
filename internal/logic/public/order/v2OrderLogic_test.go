package order

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/config"
	modelOrder "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

type providerCheckoutUsers struct{ user.Model }

func (*providerCheckoutUsers) FindOneUserSubscribe(context.Context, int64) (*user.SubscribeDetails, error) {
	return &user.SubscribeDetails{Id: 3, UserId: 7, Status: 1, EntitlementSource: "apple"}, nil
}

type providerCheckoutStore struct{ repository.Store }

func (providerCheckoutStore) User() user.Model { return &providerCheckoutUsers{} }

func TestProviderCheckoutRejectsLocalRenewalAndReset(t *testing.T) {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &user.User{Id: 7})
	service := &svc.ServiceContext{Store: providerCheckoutStore{}}
	if _, err := NewRenewalLogic(ctx, service).Renewal(&types.RenewalOrderRequest{UserSubscribeID: 3, Quantity: 1}); !errors.Is(err, user.ErrProviderManaged) {
		t.Fatalf("provider renewal error = %v", err)
	}
	if _, err := NewResetTrafficLogic(ctx, service).ResetTraffic(&types.ResetTrafficOrderRequest{UserSubscribeID: 3}); !errors.Is(err, user.ErrProviderManaged) {
		t.Fatalf("provider reset error = %v", err)
	}
}

func testV2Logic(secret string) *V2OrderLogic {
	return NewV2OrderLogic(context.Background(), &svc.ServiceContext{
		Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: secret}},
	})
}

func signedInV2Logic(secret string, userId int64) *V2OrderLogic {
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &user.User{Id: userId})
	return NewV2OrderLogic(ctx, &svc.ServiceContext{
		Config: config.Config{JwtAuth: config.JwtAuth{AccessSecret: secret}},
	})
}

// Only a purchase carries guest credentials and only an anonymous caller may
// send them, so every other combination has to be refused before a creator runs.
func TestValidateV2CreateRequestEnforcesAPerTypeShape(t *testing.T) {
	guest := &types.V2GuestOrderRequest{AuthType: "email", Identifier: "a@b.c", Password: "password123"}
	tests := []struct {
		name    string
		req     *types.V2CreateOrderRequest
		user    *user.User
		wantErr bool
	}{
		{
			name: "anonymous purchase with guest credentials",
			req:  &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1, Guest: guest},
		},
		{
			name: "signed-in purchase without guest credentials",
			req:  &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1},
			user: &user.User{Id: 7},
		},
		{
			name:    "guest credentials on a signed-in purchase",
			req:     &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1, Guest: guest},
			user:    &user.User{Id: 7},
			wantErr: true,
		},
		{
			name:    "anonymous purchase without guest credentials",
			req:     &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1},
			wantErr: true,
		},
		{
			name:    "short guest password",
			req:     &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1, Guest: &types.V2GuestOrderRequest{AuthType: "email", Identifier: "a@b.c", Password: "short"}},
			wantErr: true,
		},
		{
			name:    "purchase above the quantity limit",
			req:     &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: MaxQuantity + 1, Guest: guest},
			wantErr: true,
		},
		{
			name:    "purchase without a payment method",
			req:     &types.V2CreateOrderRequest{Type: "purchase", SubscribeID: 2, Quantity: 1, Guest: guest},
			wantErr: true,
		},
		{
			name: "renewal needs a session",
			req:  &types.V2CreateOrderRequest{Type: "renewal", PaymentID: 1, UserSubscribeID: 3, Quantity: 1},
			user: &user.User{Id: 7},
		},
		{
			name:    "anonymous renewal",
			req:     &types.V2CreateOrderRequest{Type: "renewal", PaymentID: 1, UserSubscribeID: 3, Quantity: 1},
			wantErr: true,
		},
		{
			name: "traffic reset without a coupon",
			req:  &types.V2CreateOrderRequest{Type: "reset_traffic", PaymentID: 1, UserSubscribeID: 3},
			user: &user.User{Id: 7},
		},
		{
			name:    "traffic reset with a coupon",
			req:     &types.V2CreateOrderRequest{Type: "reset_traffic", PaymentID: 1, UserSubscribeID: 3, Coupon: "FREE"},
			user:    &user.User{Id: 7},
			wantErr: true,
		},
		{
			name: "recharge with an amount",
			req:  &types.V2CreateOrderRequest{Type: "recharge", PaymentID: 1, Amount: 100},
			user: &user.User{Id: 7},
		},
		{
			name:    "recharge without an amount",
			req:     &types.V2CreateOrderRequest{Type: "recharge", PaymentID: 1},
			user:    &user.User{Id: 7},
			wantErr: true,
		},
		{
			name:    "unknown type",
			req:     &types.V2CreateOrderRequest{Type: "topup", PaymentID: 1, Amount: 100},
			user:    &user.User{Id: 7},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A trailing space and mixed case must be normalized, not rejected.
			raw := tt.req.Type
			tt.req.Type = " " + raw + " "
			err := validateV2CreateRequest(tt.req, tt.user)
			tt.req.Type = raw
			if tt.wantErr && err == nil {
				t.Fatalf("validateV2CreateRequest(%+v) = nil, want an error", tt.req)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateV2CreateRequest(%+v) = %v, want nil", tt.req, err)
			}
		})
	}
}

// The hash is what decides whether a retry is the same request or an abuse of
// the key. return_url may change on a resumed checkout; the guest block is not
// part of a signed-in request.
func TestRequestHashIgnoresReturnURLAndIrrelevantGuestData(t *testing.T) {
	logic := testV2Logic("secret")
	base := &types.V2CreateOrderRequest{Type: "purchase", PaymentID: 1, SubscribeID: 2, Quantity: 1}

	resumed := *base
	resumed.ReturnURL = "https://example.com/thanks"
	first, err := logic.requestHash(base)
	if err != nil {
		t.Fatalf("requestHash(base) error = %v", err)
	}
	second, err := logic.requestHash(&resumed)
	if err != nil {
		t.Fatalf("requestHash(resumed) error = %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("requestHash length = %d, want 64 hex characters", len(first))
	}
	if first != second {
		t.Fatalf("requestHash changed with return_url: %s != %s", first, second)
	}

	changed := *base
	changed.Quantity = 2
	third, err := logic.requestHash(&changed)
	if err != nil {
		t.Fatalf("requestHash(changed) error = %v", err)
	}
	if third == first {
		t.Fatal("requestHash ignored the quantity")
	}

	// A signed-in request must not be hashed differently because a client left
	// stale guest data in the body: the validator rejects that body outright.
	signedIn := signedInV2Logic("secret", 7)
	withoutGuest, err := signedIn.requestHash(base)
	if err != nil {
		t.Fatalf("requestHash(base) error = %v", err)
	}
	anonymous, err := testV2Logic("secret").requestHash(base)
	if err != nil {
		t.Fatalf("requestHash(base) error = %v", err)
	}
	if withoutGuest == anonymous {
		t.Fatal("requestHash ignored the caller identity")
	}
}

func TestDerivedGuestCheckoutTokenIsStableAndSecretBound(t *testing.T) {
	first := testV2Logic("secret").derivedGuestCheckoutToken("0123456789abcdef")
	second := testV2Logic("secret").derivedGuestCheckoutToken("0123456789abcdef")
	otherKey := testV2Logic("secret").derivedGuestCheckoutToken("0123456789abcdee")
	otherSecret := testV2Logic("rotated").derivedGuestCheckoutToken("0123456789abcdef")

	if first != second {
		t.Fatalf("derivedGuestCheckoutToken is not deterministic: %s != %s", first, second)
	}
	if first == otherKey {
		t.Fatal("derivedGuestCheckoutToken ignored the idempotency key")
	}
	if first == otherSecret {
		t.Fatal("derivedGuestCheckoutToken ignored the signing secret")
	}
}

func TestGuestCheckoutTokenMatchesOnlyTheStoredHash(t *testing.T) {
	token := "checkout-capability"
	owner := &modelOrder.Order{GuestCheckoutTokenHash: constant.CheckoutTokenHash(token)}
	stranger := &modelOrder.Order{GuestCheckoutTokenHash: constant.CheckoutTokenHash("someone-elses")}

	if !guestCheckoutTokenMatches(owner, token) {
		t.Fatal("guestCheckoutTokenMatches rejected the capability that created the order")
	}
	if guestCheckoutTokenMatches(owner, "wrong-capability") {
		t.Fatal("guestCheckoutTokenMatches accepted a foreign capability")
	}
	if guestCheckoutTokenMatches(stranger, token) {
		t.Fatal("guestCheckoutTokenMatches accepted a capability from another order")
	}
	// A user-owned order has no guest capability at all, so an empty token must
	// never authorize it.
	if guestCheckoutTokenMatches(&modelOrder.Order{}, "") {
		t.Fatal("guestCheckoutTokenMatches accepted an empty capability")
	}
	if got := checkoutTokenForResponse(owner, token); got != token {
		t.Fatalf("checkoutTokenForResponse(owner) = %q, want the capability", got)
	}
	if got := checkoutTokenForResponse(owner, "wrong-capability"); got != "" {
		t.Fatalf("checkoutTokenForResponse(stranger) = %q, want empty", got)
	}
}

func TestSameIdempotencyHashRejectsPrefixes(t *testing.T) {
	if !sameIdempotencyHash("abc", "abc") {
		t.Fatal("sameIdempotencyHash rejected identical hashes")
	}
	if sameIdempotencyHash("abc", "abcd") {
		t.Fatal("sameIdempotencyHash accepted different lengths")
	}
	if sameIdempotencyHash("abc", "abd") {
		t.Fatal("sameIdempotencyHash accepted different hashes")
	}
}

func TestV2OrderStatusNamesEveryStoredStatus(t *testing.T) {
	cases := map[uint8]string{
		modelOrder.StatusPending:  "pending_payment",
		modelOrder.StatusPaid:     "paid",
		modelOrder.StatusClosed:   "closed",
		modelOrder.StatusFailed:   "failed",
		modelOrder.StatusFinished: "finished",
		0:                         "unknown",
	}
	for status, want := range cases {
		if got := v2OrderStatus(status); got != want {
			t.Fatalf("v2OrderStatus(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestGuestReplayLimitsBeforeFingerprintComparison(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	logic := NewV2OrderLogic(requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "test")), &svc.ServiceContext{Redis: client})
	current := &modelOrder.Order{IdempotencyHash: "original"}
	for attempt := 0; attempt < 10; attempt++ {
		if _, err := logic.replayCreate(current, &types.V2CreateOrderRequest{}, "key", "wrong"); err != ErrIdempotencyKeyReused {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err := logic.replayCreate(current, &types.V2CreateOrderRequest{}, "key", "wrong"); err == nil || err == ErrIdempotencyKeyReused {
		t.Fatal("guest fingerprint oracle was not limited")
	}
	client.Close()
	if err := logic.limitGuestReplay("another-key"); err == nil {
		t.Fatal("Redis failure allowed a guest replay")
	}
}

func TestGuestReplayIPQuotaAndSignedInOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	logic := NewV2OrderLogic(context.Background(), &svc.ServiceContext{Redis: client})
	for attempt := 0; attempt < 60; attempt++ {
		if err := logic.limitGuestReplay(string(rune(1000 + attempt))); err != nil {
			t.Fatal(err)
		}
	}
	if err := logic.limitGuestReplay("last"); err == nil {
		t.Fatal("rotating keys bypassed the client IP quota")
	}
	signedIn := signedInV2Logic("secret", 7)
	_, err := signedIn.replayCreate(&modelOrder.Order{UserId: 8, IdempotencyHash: "original"}, &types.V2CreateOrderRequest{}, "key", "wrong")
	var coded *xerr.CodeError
	if !errors.As(err, &coded) || coded.GetErrCode() != xerr.InvalidAccess {
		t.Fatalf("foreign account reached fingerprint comparison: %v", err)
	}
}
