package entitlement

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	_ "modernc.org/sqlite"
)

type reconcilerFixture struct {
	client *ent.Client
	store  repository.Store
	now    time.Time
}

var fixtureSequence atomic.Uint64

type entitlementCacheStore struct {
	repository.Store
	plans *entitlementCachePlans
}

func (store entitlementCacheStore) Subscribe() subscribe.Model { return store.plans }

type entitlementCachePlans struct {
	subscribe.Model
	client  *ent.Client
	failure error
	cleared []int64
}

func (plans *entitlementCachePlans) ClearCache(ctx context.Context, planIDs ...int64) error {
	if _, err := plans.client.SubscriptionEntitlement.Query().Only(ctx); err != nil {
		return fmt.Errorf("cache invalidation ran before entitlement commit: %w", err)
	}
	plans.cleared = append([]int64(nil), planIDs...)
	return plans.failure
}

func TestReconcileCacheFailureRetriesCommittedHistory(t *testing.T) {
	fixture := newReconcilerFixture(t)
	ctx := context.Background()
	cacheFailure := errors.New("cache unavailable")
	plans := &entitlementCachePlans{Model: fixture.store.Subscribe(), client: fixture.client, failure: cacheFailure}
	reconciler := NewReconciler(entitlementCacheStore{Store: fixture.store, plans: plans})
	command := fixture.command()
	if _, err := reconciler.Reconcile(ctx, command); !errors.Is(err, cacheFailure) {
		t.Fatalf("first cache failure = %v, want %v", err, cacheFailure)
	}
	first := fixture.client.UserSubscribe.Query().OnlyX(ctx)
	for revision := int64(2); revision <= 3; revision++ {
		fixture.client.Subscribe.Create().SetID(revision).SetTraffic(100).SaveX(ctx)
		transactionKey := fmt.Sprintf("tx-%d", revision)
		fixture.client.Order.Create().SetID(revision).SetUserID(command.UserID).
			SetOrderNo(fmt.Sprintf("order-%d", revision)).SetMethod(AppleIAPOrderMethod).
			SetTradeNo(transactionKey).SetStatus(2).SetSubscribeID(revision).SaveX(ctx)
		next := command
		next.Revision, next.PlanID, next.OrderID = revision, revision, revision
		next.TransactionKey = transactionKey
		if _, err := reconciler.Reconcile(ctx, next); !errors.Is(err, cacheFailure) {
			t.Fatalf("revision %d cache failure = %v", revision, err)
		}
	}
	if got := fixture.client.SubscriptionPeriod.Query().CountX(ctx); got != 3 {
		t.Fatalf("periods after cache failures = %d, want 3", got)
	}
	fixture.client.UserSubscribe.UpdateOneID(first.ID).SetUpload(70).SetDownload(30).SetStatus(2).ExecX(ctx)
	plans.failure = nil
	result, err := reconciler.Reconcile(ctx, command)
	if err != nil {
		t.Fatalf("stale replay cache retry: %v", err)
	}
	if result.Applied || result.Revision != 3 || result.UserSubscribeID != first.ID {
		t.Fatalf("cache retry reapplied snapshot: %+v", result)
	}
	seen := make(map[int64]bool)
	for _, planID := range plans.cleared {
		seen[planID] = true
	}
	if len(seen) != 3 || !seen[1] || !seen[2] || !seen[3] {
		t.Fatalf("cache retry missed historical plans: %v", plans.cleared)
	}
	current := fixture.client.UserSubscribe.GetX(ctx, first.ID)
	if current.SubscribeID != 3 || current.Upload != 70 || current.Download != 30 || current.Status != 2 {
		t.Fatalf("cache retry changed projection or usage: %+v", current)
	}
	if got := fixture.client.SubscriptionEntitlementRevision.Query().CountX(ctx); got != 3 {
		t.Fatalf("replay inserted another revision: %d", got)
	}
	fixture.client.Subscribe.DeleteOneID(2).ExecX(ctx)
	if _, err := reconciler.Reconcile(ctx, command); err != nil {
		t.Fatalf("deleted historical plan blocked replay: %v", err)
	}
}

func newReconcilerFixture(t *testing.T) *reconcilerFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:entitlement-test-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", fixtureSequence.Add(1))
	database, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixture := &reconcilerFixture{client: client, store: repository.NewStore(client, nil), now: now}

	if _, err := client.User.Create().SetID(7).SetPassword("hash").SetReferCode("user-7").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Subscribe.Create().SetID(1).SetName("monthly").SetTraffic(100).SetTrafficUnlimited(false).SetNodeGroupID(1).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Order.Create().SetID(1).SetUserID(7).SetOrderNo("order-1").SetMethod(AppleIAPOrderMethod).SetTradeNo("tx-1").SetStatus(2).SetSubscribeID(1).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *reconcilerFixture) command() ReconcileEntitlementCommand {
	return ReconcileEntitlementCommand{
		Source:          SourceApple,
		Scope:           "app.example:Production",
		SubscriptionKey: "original-7",
		TransactionKey:  "tx-1",
		UserID:          7,
		PlanID:          1,
		OrderID:         1,
		Revision:        1,
		Mode:            ModeAutoRenewable,
		BillingInterval: BillingIntervalMonth,
		Status:          StatusActive,
		PeriodStart:     f.now.Add(-48 * time.Hour),
		PeriodEnd:       f.now.Add(28 * 24 * time.Hour),
		AutoRenew:       true,
		ResetTraffic:    true,
	}
}

func TestReconcileEntitlementPersistsProjectionAndReplayIsSafe(t *testing.T) {
	f := newReconcilerFixture(t)
	reconciler := NewReconciler(f.store)
	command := f.command()

	first, err := reconciler.Reconcile(context.Background(), command)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if !first.Applied || first.Revision != 1 || !first.AccessUntil.Equal(command.PeriodEnd) {
		t.Fatalf("unexpected first result: %+v", first)
	}
	subscription, err := f.client.UserSubscribe.Get(context.Background(), first.UserSubscribeID)
	if err != nil {
		t.Fatal(err)
	}
	if subscription.EntitlementSource != SourceApple || subscription.Status != 1 || !subscription.ExpireTime.Equal(command.PeriodEnd) {
		t.Fatalf("unexpected provider subscription: %+v", subscription)
	}
	if got, err := f.client.SubscriptionEntitlement.Query().Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("entitlement count = %d, err = %v", got, err)
	}
	if got, err := f.client.SubscriptionPeriod.Query().Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("period count = %d, err = %v", got, err)
	}
	if got, err := f.client.SubscriptionEntitlementRevision.Query().Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("revision count = %d, err = %v", got, err)
	}

	subscription = subscription.Update().SetUpload(60).SetDownload(40).SetStatus(2).SaveX(context.Background())
	replay, err := reconciler.Reconcile(context.Background(), command)
	if err != nil {
		t.Fatalf("replay reconcile: %v", err)
	}
	if replay.Applied || replay.UserSubscribeID != first.UserSubscribeID || replay.Revision != first.Revision {
		t.Fatalf("unexpected replay result: %+v", replay)
	}
	subscription = f.client.UserSubscribe.GetX(context.Background(), first.UserSubscribeID)
	if subscription.Upload != 60 || subscription.Download != 40 || subscription.Status != 2 {
		t.Fatalf("replay changed usage or status: %+v", subscription)
	}
}

func TestReconcileEntitlementRejectsStaleAndConflictingRevision(t *testing.T) {
	f := newReconcilerFixture(t)
	reconciler := NewReconciler(f.store)
	command := f.command()
	if _, err := reconciler.Reconcile(context.Background(), command); err != nil {
		t.Fatal(err)
	}

	newer := command
	newer.Revision = 2
	newer.Status = StatusExpired
	newer.PeriodEnd = f.now.Add(-time.Hour)
	if _, err := reconciler.Reconcile(context.Background(), newer); err != nil {
		t.Fatalf("newer revision: %v", err)
	}

	stale := command
	staleResult, err := reconciler.Reconcile(context.Background(), stale)
	if err != nil {
		t.Fatalf("stale revision: %v", err)
	}
	if staleResult.Applied || staleResult.Revision != newer.Revision {
		t.Fatalf("unexpected stale result: %+v", staleResult)
	}
	subscription := f.client.UserSubscribe.GetX(context.Background(), staleResult.UserSubscribeID)
	if subscription.Status != 3 || !subscription.ExpireTime.Equal(newer.PeriodEnd) {
		t.Fatalf("stale revision changed projection: %+v", subscription)
	}

	conflict := command
	conflict.Scope = "other-environment"
	conflictResult, err := reconciler.Reconcile(context.Background(), conflict)
	if err != nil {
		t.Fatalf("independent entitlement scope: %v", err)
	}
	if !conflictResult.Applied || conflictResult.UserSubscribeID == staleResult.UserSubscribeID {
		t.Fatalf("scope was not isolated: %+v", conflictResult)
	}
	if got, err := f.client.SubscriptionEntitlement.Query().Count(context.Background()); err != nil || got != 2 {
		t.Fatalf("entitlement count after independent scope = %d, err = %v", got, err)
	}

	sameRevision := newer
	sameRevision.Status = StatusGrace
	graceUntil := newer.PeriodEnd.Add(24 * time.Hour)
	sameRevision.GraceUntil = &graceUntil
	if _, err := reconciler.Reconcile(context.Background(), sameRevision); !errors.Is(err, ErrEntitlementConflict) {
		t.Fatalf("same-revision conflict error = %v, want ErrEntitlementConflict", err)
	}
}

func TestReconcileEntitlementHonorsSingleModelAndQuotaStatus(t *testing.T) {
	f := newReconcilerFixture(t)
	if err := f.client.Subscribe.UpdateOneID(1).SetQuota(1).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.UserSubscribe.Create().
		SetUserID(7).
		SetOrderID(1).
		SetSubscribeID(1).
		SetStartTime(f.now.Add(-time.Hour)).
		SetExpireTime(f.now.Add(time.Hour)).
		SetTraffic(100).
		SetToken("deducted-token").
		SetUUID("deducted-uuid").
		SetStatus(4).
		Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReconciler(f.store).Reconcile(context.Background(), f.command()); err != nil {
		t.Fatalf("deducted subscription consumed quota: %v", err)
	}

	second := newReconcilerFixture(t)
	if _, err := second.client.UserSubscribe.Create().
		SetUserID(7).
		SetOrderID(1).
		SetSubscribeID(1).
		SetStartTime(second.now.Add(-time.Hour)).
		SetExpireTime(second.now.Add(time.Hour)).
		SetTraffic(100).
		SetToken("active-token").
		SetUUID("active-uuid").
		SetStatus(1).
		Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReconciler(second.store, func() bool { return true }).Reconcile(context.Background(), second.command()); err == nil {
		t.Fatal("single subscription mode accepted an existing blocking subscription")
	}
}

func TestReconcileEntitlementProjectsGraceRevocationAndNewPeriod(t *testing.T) {
	f := newReconcilerFixture(t)
	reconciler := NewReconciler(f.store)
	command := f.command()
	first, err := reconciler.Reconcile(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}

	grace := command
	grace.Revision = 2
	grace.Status = StatusGrace
	graceUntil := command.PeriodEnd.Add(48 * time.Hour)
	grace.GraceUntil = &graceUntil
	graceResult, err := reconciler.Reconcile(context.Background(), grace)
	if err != nil {
		t.Fatalf("grace reconcile: %v", err)
	}
	if !graceResult.AccessUntil.Equal(graceUntil) {
		t.Fatalf("grace access until = %v, want %v", graceResult.AccessUntil, graceUntil)
	}
	subscription := f.client.UserSubscribe.GetX(context.Background(), first.UserSubscribeID)
	if subscription.Status != 1 || !subscription.ExpireTime.Equal(graceUntil) {
		t.Fatalf("grace projection: %+v", subscription)
	}

	revoked := grace
	revoked.Revision = 3
	revoked.Status = StatusRevoked
	revoked.GraceUntil = nil
	revokedAt := f.now.Add(-time.Hour)
	revoked.RevokedAt = &revokedAt
	revokedResult, err := reconciler.Reconcile(context.Background(), revoked)
	if err != nil {
		t.Fatalf("revocation reconcile: %v", err)
	}
	if !revokedResult.AccessUntil.Equal(revokedAt) {
		t.Fatalf("revoked access until = %v, want %v", revokedResult.AccessUntil, revokedAt)
	}
	subscription = f.client.UserSubscribe.GetX(context.Background(), first.UserSubscribeID)
	if subscription.Status != 3 || !subscription.ExpireTime.Equal(revokedAt) {
		t.Fatalf("revocation projection: %+v", subscription)
	}

	second := f.command()
	second.Revision = 4
	second.TransactionKey = "tx-2"
	second.OrderID = 2
	second.PeriodStart = f.now.Add(-time.Hour)
	second.PeriodEnd = f.now.Add(30 * 24 * time.Hour)
	if _, err := f.client.Order.Create().SetID(2).SetUserID(7).SetOrderNo("order-2").SetMethod(AppleIAPOrderMethod).SetTradeNo("tx-2").SetStatus(2).SetSubscribeID(1).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondResult, err := reconciler.Reconcile(context.Background(), second)
	if err != nil {
		t.Fatalf("new period reconcile: %v", err)
	}
	if secondResult.UserSubscribeID != first.UserSubscribeID {
		t.Fatalf("new period created subscription %d, want %d", secondResult.UserSubscribeID, first.UserSubscribeID)
	}
	if got, err := f.client.SubscriptionPeriod.Query().Count(context.Background()); err != nil || got != 2 {
		t.Fatalf("period count after renewal = %d, err = %v", got, err)
	}
}

func TestReconcileEntitlementRejectsOrderOwnershipMismatch(t *testing.T) {
	f := newReconcilerFixture(t)
	command := f.command()
	command.UserID = 8
	command.Revision = 1
	command.PeriodStart = f.now.Add(-time.Hour)
	command.PeriodEnd = f.now.Add(time.Hour)
	if _, err := NewReconciler(f.store).Reconcile(context.Background(), command); err == nil {
		t.Fatal("ownership mismatch was accepted")
	} else if !errors.Is(err, ErrEntitlementConflict) {
		t.Fatalf("ownership mismatch error = %v", err)
	}
}

func TestEntitlementKeyIsStableAndSeparated(t *testing.T) {
	first := entitlementKey(SourceApple, "scope", "subscription")
	if first != entitlementKey(SourceApple, "scope", "subscription") {
		t.Fatal("entitlement key is not stable")
	}
	if first == entitlementKey(SourceApple, "scope", "other-subscription") {
		t.Fatal("different subscriptions share an entitlement key")
	}
	if len(first) != 64 {
		t.Fatalf("entitlement key length = %d", len(first))
	}
}
