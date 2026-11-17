package user

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/pkg/authmethod"
)

func TestCanonicalAuthIdentifier(t *testing.T) {
	got, err := canonicalAuthIdentifier(authmethod.Email, "  User@Example.COM ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "user@example.com" {
		t.Fatalf("canonicalAuthIdentifier(email) = %q, want user@example.com", got)
	}

	if _, err := canonicalAuthIdentifier(authmethod.Email, "   "); !errors.Is(err, ErrInvalidEmailIdentity) {
		t.Fatalf("blank email should return ErrInvalidEmailIdentity, got %v", err)
	}

	// Non-email identifiers are never rejected even when empty (device rows etc.).
	got, err = canonicalAuthIdentifier("github", "")
	if err != nil {
		t.Fatalf("non-email identifier should not error: %v", err)
	}
	if got != "" {
		t.Fatalf("non-email identifier should be verbatim, got %q", got)
	}
}

func TestHasConflictingEmailIdentity(t *testing.T) {
	// Only the row with the same id is allowed; any other id is a conflict.
	if !hasConflictingEmailIdentity(1, []*ent.UserAuthMethod{{ID: 2}}) {
		t.Fatal("different id must be a conflict")
	}
	if !hasConflictingEmailIdentity(0, []*ent.UserAuthMethod{{ID: 5}, {ID: 6}}) {
		t.Fatal("pre-insert (id=0) with existing rows must conflict")
	}
	if hasConflictingEmailIdentity(7, []*ent.UserAuthMethod{{ID: 7}}) {
		t.Fatal("same id must not conflict")
	}
	if hasConflictingEmailIdentity(7, nil) {
		t.Fatal("no rows must not conflict")
	}
}

func TestResolveUniqueAuthMethod(t *testing.T) {
	if _, err := resolveUniqueAuthMethod(nil); !ent.IsNotFound(err) {
		t.Fatalf("zero rows should be NotFound, got %v", err)
	}

	one, err := resolveUniqueAuthMethod([]*ent.UserAuthMethod{{ID: 3, AuthIdentifier: "a@b.c"}})
	if err != nil {
		t.Fatalf("single row should resolve: %v", err)
	}
	if one == nil || one.Id != 3 {
		t.Fatalf("single row resolved incorrectly: %+v", one)
	}

	if _, err := resolveUniqueAuthMethod([]*ent.UserAuthMethod{{ID: 1}, {ID: 2}}); !errors.Is(err, ErrAmbiguousEmailIdentity) {
		t.Fatalf("two rows should be ambiguous, got %v", err)
	}
}

type recipientQueryDriver struct {
	dialect.Driver
	query string
}

func (driver *recipientQueryDriver) Dialect() string { return dialect.MySQL }

func (driver *recipientQueryDriver) Query(_ context.Context, query string, _ any, _ any) error {
	driver.query = query
	return errors.New("query captured")
}

func (driver *recipientQueryDriver) Exec(_ context.Context, query string, _ any, _ any) error {
	driver.query = query
	return errors.New("query captured")
}

func TestEmailRecipientsExcludeDeletedOwners(t *testing.T) {
	for scope := 1; scope <= 4; scope++ {
		driver := &recipientQueryDriver{}
		model := &customUserModel{defaultUserModel: newUserModel(ent.NewClient(ent.Driver(driver)), nil)}
		filter := &EmailRecipientFilter{Scope: int8(scope)}
		_, _ = model.QueryEmailRecipients(context.Background(), filter)
		if !strings.Contains(driver.query, "JOIN `user`") || !strings.Contains(driver.query, "`.`deleted_at` IS NULL") {
			t.Fatalf("scope %d includes deleted recipients: %s", scope, driver.query)
		}
		_, _ = model.CountEmailRecipients(context.Background(), filter)
		if !strings.Contains(driver.query, "JOIN `user`") || !strings.Contains(driver.query, "`.`deleted_at` IS NULL") {
			t.Fatalf("scope %d counts deleted recipients: %s", scope, driver.query)
		}
	}
}

func TestNotificationIdentityExcludesDeletedOwner(t *testing.T) {
	driver := &recipientQueryDriver{}
	model := newUserModel(ent.NewClient(ent.Driver(driver)), nil)
	_, _ = model.FindUserAuthMethodByUserId(context.Background(), "email", 7)
	if !strings.Contains(driver.query, "JOIN `user`") || !strings.Contains(driver.query, "`.`deleted_at` IS NULL") {
		t.Fatalf("notification identity includes a deleted owner: %s", driver.query)
	}
}

func TestSubscriptionFinishRechecksCurrentEligibility(t *testing.T) {
	for _, status := range []uint8{2, 3} {
		driver := &recipientQueryDriver{}
		client := ent.NewClient(ent.Driver(driver))
		conditions, err := finishConditions(status, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = client.UserSubscribe.Query().Where(conditions...).All(context.Background())
		if !strings.Contains(driver.query, "`status` IN") {
			t.Fatalf("held subscriptions can be finished: %s", driver.query)
		}
		if status == 2 && (!strings.Contains(driver.query, "`traffic_unlimited`") || !strings.Contains(driver.query, "`upload` +")) {
			t.Fatalf("traffic no longer checked: %s", driver.query)
		}
		if status == 3 && (!strings.Contains(driver.query, "`expire_time` <") || !strings.Contains(driver.query, "`finished_at` IS NULL")) {
			t.Fatalf("renewed subscription can be expired: %s", driver.query)
		}
	}
	if _, err := finishConditions(5, time.Now()); err == nil {
		t.Fatal("unsupported status accepted")
	}
}

func TestSubscriptionRenewalAndResetWriteOnlyOwnedFields(t *testing.T) {
	for _, reset := range []bool{false, true} {
		driver := &recipientQueryDriver{}
		model := newUserModel(ent.NewClient(ent.Driver(driver)), nil)
		_ = model.RenewSubscribe(context.Background(), &Subscribe{Id: 3, UserId: 7, SubscribeId: 9, ExpireTime: time.Now()}, reset)
		for _, protected := range []string{"token", "uuid", "node_group_id", "group_locked", "note", "order_id"} {
			if strings.Contains(driver.query, "`"+protected+"` =") {
				t.Fatalf("renewal overwrites %s: %s", protected, driver.query)
			}
		}
		if strings.Contains(driver.query, "`upload` =") != reset {
			t.Fatalf("renewal reset %t: %s", reset, driver.query)
		}
		if !strings.Contains(driver.query, "`status` <") || !strings.Contains(driver.query, "`user_id` =") {
			t.Fatalf("renewal lacks owner or terminal-state guard: %s", driver.query)
		}
		_ = model.ResetSubscribeTraffic(context.Background(), 3)
		if strings.Contains(driver.query, "`expire_time` =") || strings.Contains(driver.query, "`token` =") || !strings.Contains(driver.query, "`finished_at` = NULL") {
			t.Fatalf("traffic reset writes the wrong fields: %s", driver.query)
		}
	}
}

func TestSubscriptionCredentialNoteAndStatusWritesAreScoped(t *testing.T) {
	driver := &recipientQueryDriver{}
	model := newUserModel(ent.NewClient(ent.Driver(driver)), nil)
	_ = model.RotateSubscribeCredentials(context.Background(), 3, 7, "random-token", "random-uuid")
	if !strings.Contains(driver.query, "`token` =") || !strings.Contains(driver.query, "`uuid` =") || !strings.Contains(driver.query, "`user_id` =") {
		t.Fatalf("rotation lacks credentials or owner guard: %s", driver.query)
	}
	for _, protected := range []string{"upload", "download", "expire_time", "status", "node_group_id", "note"} {
		if strings.Contains(driver.query, "`"+protected+"` =") {
			t.Fatalf("rotation overwrites %s: %s", protected, driver.query)
		}
	}
	_ = model.UpdateSubscribeNote(context.Background(), 3, 7, "new note")
	if strings.Contains(driver.query, "`uuid` =") || strings.Contains(driver.query, "`status` =") || !strings.Contains(driver.query, "`note` =") {
		t.Fatalf("note writes unrelated fields: %s", driver.query)
	}
	_, _ = model.CompareSubscribeStatus(context.Background(), 3, 1, 5)
	if strings.Count(driver.query, "`status`") != 2 || strings.Contains(driver.query, "`token` =") {
		t.Fatalf("status update lacks CAS or overwrites credentials: %s", driver.query)
	}
	_ = model.UpdateSubscribeSettings(context.Background(), &Subscribe{Id: 3, SubscribeId: 9, Status: 1, ExpireTime: time.Now()}, 1)
	for _, protected := range []string{"token", "uuid", "note", "node_group_id", "group_locked", "order_id", "start_time"} {
		if strings.Contains(driver.query, "`"+protected+"` =") {
			t.Fatalf("admin edit overwrites %s: %s", protected, driver.query)
		}
	}
	if strings.Count(driver.query, "`status`") != 2 {
		t.Fatalf("admin edit lacks status CAS: %s", driver.query)
	}
	_ = model.RedeemSubscribe(context.Background(), &Subscribe{Id: 3, UserId: 7, OrderId: 9, ExpireTime: time.Now()}, true)
	for _, protected := range []string{"token", "uuid", "note", "node_group_id", "group_locked", "start_time"} {
		if strings.Contains(driver.query, "`"+protected+"` =") {
			t.Fatalf("redemption overwrites %s: %s", protected, driver.query)
		}
	}
	if !strings.Contains(driver.query, "`status` <") {
		t.Fatalf("redemption lacks terminal-state guard: %s", driver.query)
	}
}
