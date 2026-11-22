package orderflow

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func TestReserveCouponEligibilityAndRollback(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	store := repository.NewStore(client, nil)

	now := time.Now()
	for _, tc := range []struct {
		name          string
		configure     func(*ent.CouponCreate) *ent.CouponCreate
		purchase      order.Order
		existing      func(*ent.OrderCreate) *ent.OrderCreate
		wantCode      uint32
		wantExhausted bool
		wantUsed      int64
	}{
		{name: "disabled", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetEnable(false) }, wantCode: xerr.InvalidParams},
		{name: "future", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetStartTime(now.Add(time.Hour).Unix()) }, wantCode: xerr.InvalidParams},
		{name: "expired", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetExpireTime(now.Add(-time.Hour).Unix()) }, wantCode: xerr.InvalidParams},
		{name: "different plan", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetSubscribe("12,30") }, purchase: order.Order{SubscribeId: 2}, wantCode: xerr.CouponNotApplicable},
		{name: "globally exhausted", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetCount(1).SetUsedCount(1) }, wantExhausted: true, wantUsed: 1},
		{
			name:      "account quota",
			configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetUserLimit(1) },
			purchase:  order.Order{UserId: 7},
			existing:  func(o *ent.OrderCreate) *ent.OrderCreate { return o.SetUserID(7).SetStatus(order.StatusPending) },
			wantCode:  xerr.CouponInsufficientUsage,
		},
		{
			name:      "guest quota uses canonical email",
			configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetUserLimit(1) },
			purchase:  order.Order{GuestAuthType: "email", GuestIdentifier: "Buyer@Example.com"},
			existing: func(o *ent.OrderCreate) *ent.OrderCreate {
				return o.SetGuestAuthType("email").SetGuestIdentifier("buyer@example.com").SetStatus(order.StatusPaid)
			},
			wantCode: xerr.CouponInsufficientUsage,
		},
		{name: "guest with no identity", configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetUserLimit(1) }, wantCode: xerr.CouponInsufficientUsage},
		{
			name:      "eligible account and allowed plan",
			configure: func(c *ent.CouponCreate) *ent.CouponCreate { return c.SetSubscribe("12,30").SetUserLimit(1) },
			purchase:  order.Order{SubscribeId: 12, UserId: 7},
			existing:  func(o *ent.OrderCreate) *ent.OrderCreate { return o.SetUserID(9).SetStatus(order.StatusPaid) },
			wantUsed:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coupon := tc.configure(client.Coupon.Create().SetCode(tc.name)).SaveX(ctx)
			if tc.existing != nil {
				tc.existing(client.Order.Create().SetCoupon(coupon.Code)).SaveX(ctx)
			}
			purchase := tc.purchase
			purchase.Coupon = coupon.Code
			err := store.InTx(ctx, func(tx repository.Store) error { return ReserveCoupon(ctx, tx, &purchase) })
			switch {
			case tc.wantCode != 0:
				var codeErr *xerr.CodeError
				if !errors.As(err, &codeErr) || codeErr.GetErrCode() != tc.wantCode {
					t.Fatalf("ReserveCoupon error = %v, want code %d", err, tc.wantCode)
				}
			case tc.wantExhausted:
				if err == nil {
					t.Fatal("globally exhausted coupon was accepted")
				}
			case err != nil:
				t.Fatalf("ReserveCoupon: %v", err)
			}
			if used := client.Coupon.GetX(ctx, coupon.ID).UsedCount; used != tc.wantUsed || purchase.CouponReserved != (err == nil) {
				t.Fatalf("used = %d, reserved = %t, want used = %d, reserved = %t", used, purchase.CouponReserved, tc.wantUsed, err == nil)
			}
		})
	}

	if err := ReserveCoupon(ctx, store, &order.Order{Coupon: "missing"}); !ent.IsNotFound(err) {
		t.Fatalf("missing coupon error = %v, want not found", err)
	}
	purchase := &order.Order{}
	if err := ReserveCoupon(ctx, nil, purchase); err != nil || purchase.CouponReserved {
		t.Fatalf("checkout without a coupon mutated the order or failed: %+v, %v", purchase, err)
	}
}
