package withdrawal

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

var fixtureSequence atomic.Uint64

type fixture struct {
	client   *ent.Client
	database *stdsql.DB
	store    repository.Store
	svcCtx   *svc.ServiceContext
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := fmt.Sprintf("file:withdrawal-test-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", fixtureSequence.Add(1))
	database, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })

	store := repository.NewStore(client, nil)
	return &fixture{client: client, database: database, store: store, svcCtx: &svc.ServiceContext{Store: store, Ent: client}}
}

// seedUser creates a user with the given commission balance and a pending
// withdrawal of `amount`.
func (f *fixture) seedUser(t *testing.T, id, commission, amount int64) *user.Withdrawal {
	t.Helper()
	ctx := context.Background()
	if _, err := f.client.User.Create().SetID(id).SetPassword("hash").SetReferCode(fmt.Sprintf("user-%d", id)).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.UserWallet.Create().SetUserID(id).SetBalance(0).SetGiftAmount(0).SetCommission(commission).Save(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal := &user.Withdrawal{UserId: id, Amount: amount, Content: "bank", Status: types.WithdrawalStatusPending}
	if err := f.store.User().InsertWithdrawal(ctx, withdrawal); err != nil {
		t.Fatal(err)
	}
	return withdrawal
}

func (f *fixture) commission(t *testing.T, userID int64) int64 {
	t.Helper()
	wallet, err := f.store.Wallet().FindOne(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return wallet.Commission
}

func (f *fixture) status(t *testing.T, id int64) (uint8, string) {
	t.Helper()
	row, err := f.client.UserWithdrawal.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row.Status, row.Reason
}

func (f *fixture) commissionLogs(t *testing.T, userID int64) []*modellog.SystemLog {
	t.Helper()
	rows, _, err := f.store.Log().FilterSystemLog(context.Background(), &modellog.FilterParams{
		Page: 1, Size: 50, Type: modellog.TypeCommission.Uint8(), ObjectID: userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Withdrawal writes must land in the same table the migrations create.
func TestWithdrawalStoreUsesMigratedTable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.client.User.Create().SetID(1).SetPassword("h").SetReferCode("u1").Save(ctx); err != nil {
		t.Fatal(err)
	}
	withdrawal := &user.Withdrawal{UserId: 1, Amount: 100, Content: "c", Status: types.WithdrawalStatusPending}
	if err := f.store.User().InsertWithdrawal(ctx, withdrawal); err != nil {
		t.Fatal(err)
	}
	var table string
	rows, err := f.database.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE '%withdrawal%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if table != "withdrawals" {
		t.Fatalf("withdrawal rows live in %q, want %q", table, "withdrawals")
	}
}

// A rejection returns the amount to the commission balance and records it.
func TestRejectionRefundsCommissionAndLogs(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, 100, 400)

	logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "  details do not match  "}); err != nil {
		t.Fatalf("review: %v", err)
	}

	if got := f.commission(t, 7); got != 500 {
		t.Fatalf("commission = %d, want 500", got)
	}
	status, reason := f.status(t, withdrawal.Id)
	if status != types.WithdrawalStatusRejected || reason != "details do not match" {
		t.Fatalf("status/reason = %d/%q, want 2/%q", status, reason, "details do not match")
	}
	logs := f.commissionLogs(t, 7)
	if len(logs) != 1 {
		t.Fatalf("commission logs = %d, want 1", len(logs))
	}
	var entry modellog.Commission
	if err := entry.Unmarshal([]byte(logs[0].Content)); err != nil {
		t.Fatalf("decode commission log: %v", err)
	}
	if entry.Type != modellog.CommissionTypeWithdraw || entry.Amount != 400 || entry.Balance != 500 {
		t.Fatalf("commission log = %+v, want type=334 amount=400 balance=500", entry)
	}
}

// An approval closes the request without touching the money: the commission was
// already debited when the user asked to withdraw.
func TestApprovalKeepsCommissionAndClearsReason(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, 100, 400)

	logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusApproved, Reason: "ignored"}); err != nil {
		t.Fatalf("review: %v", err)
	}

	if got := f.commission(t, 7); got != 100 {
		t.Fatalf("commission = %d, want 100", got)
	}
	status, reason := f.status(t, withdrawal.Id)
	if status != types.WithdrawalStatusApproved || reason != "" {
		t.Fatalf("status/reason = %d/%q, want 1/empty", status, reason)
	}
	if logs := f.commissionLogs(t, 7); len(logs) != 0 {
		t.Fatalf("an approval must not write a commission log, got %d", len(logs))
	}
}

// A rejection needs a non-empty reason, and an unknown status is refused.
func TestReviewValidatesItsInput(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, 100, 400)
	logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)

	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "   "}); err == nil {
		t.Fatal("a whitespace-only rejection reason must be refused")
	}
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: 0}); err == nil {
		t.Fatal("status 0 (pending) is not a review outcome and must be refused")
	}
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: 9}); err == nil {
		t.Fatal("an unknown status must be refused")
	}
	// None of the refusals may have moved the money or the row.
	if got := f.commission(t, 7); got != 100 {
		t.Fatalf("commission = %d, want 100", got)
	}
	if status, _ := f.status(t, withdrawal.Id); status != types.WithdrawalStatusPending {
		t.Fatalf("status = %d, want still pending", status)
	}
}

// Reviewing the same request twice must not refund twice.
func TestSecondReviewIsRefusedWithoutASecondRefund(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, 100, 400)
	logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)

	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "no"}); err != nil {
		t.Fatalf("first review: %v", err)
	}
	err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "no again"})
	if err == nil {
		t.Fatal("a second review of the same request must be refused")
	}

	if got := f.commission(t, 7); got != 500 {
		t.Fatalf("commission = %d, want 500 (refunded exactly once)", got)
	}
	if logs := f.commissionLogs(t, 7); len(logs) != 1 {
		t.Fatalf("commission logs = %d, want exactly 1", len(logs))
	}
}

// Two reviewers racing on one request: exactly one wins, and the refund happens
// once.
func TestConcurrentReviewsRefundOnce(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, 100, 400)

	const reviewers = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
	)
	start := make(chan struct{})
	for i := 0; i < reviewers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)
			err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "race"})
			if err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeeded != 1 {
		t.Fatalf("%d reviewers succeeded, want exactly 1", succeeded)
	}
	if got := f.commission(t, 7); got != 500 {
		t.Fatalf("commission = %d, want 500 (one refund)", got)
	}
	if logs := f.commissionLogs(t, 7); len(logs) != 1 {
		t.Fatalf("commission logs = %d, want exactly 1", len(logs))
	}
}

// The list is filtered by status and user, and reports the unpaginated total.
func TestListFiltersAndReportsTotal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.seedUser(t, 7, 0, 400)
	if _, err := f.client.User.Create().SetID(8).SetPassword("h").SetReferCode("u8").Save(ctx); err != nil {
		t.Fatal(err)
	}
	second := &user.Withdrawal{UserId: 8, Amount: 900, Content: "c", Status: types.WithdrawalStatusPending}
	if err := f.store.User().InsertWithdrawal(ctx, second); err != nil {
		t.Fatal(err)
	}
	logic := NewReviewWithdrawalLogic(ctx, f.svcCtx)
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: first.Id, Status: types.WithdrawalStatusApproved}); err != nil {
		t.Fatal(err)
	}

	listLogic := NewGetWithdrawalListLogic(ctx, f.svcCtx)

	all, err := listLogic.GetWithdrawalList(&types.GetWithdrawalListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 || len(all.List) != 2 {
		t.Fatalf("unfiltered = %d/%d, want 2/2", all.Total, len(all.List))
	}

	pending := types.WithdrawalStatusPending
	onlyPending, err := listLogic.GetWithdrawalList(&types.GetWithdrawalListRequest{Page: 1, Size: 10, Status: &pending})
	if err != nil {
		t.Fatal(err)
	}
	if onlyPending.Total != 1 || len(onlyPending.List) != 1 || onlyPending.List[0].UserId != 8 {
		t.Fatalf("pending filter = %+v, want just user 8", onlyPending)
	}

	userID := int64(7)
	onlyUser, err := listLogic.GetWithdrawalList(&types.GetWithdrawalListRequest{Page: 1, Size: 10, UserId: &userID})
	if err != nil {
		t.Fatal(err)
	}
	if onlyUser.Total != 1 || onlyUser.List[0].Id != first.Id {
		t.Fatalf("user filter = %+v, want just withdrawal %d", onlyUser, first.Id)
	}

	// The page size is clamped so one request cannot drain the table.
	huge, err := listLogic.GetWithdrawalList(&types.GetWithdrawalListRequest{Page: 1, Size: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(huge.List) > MaxWithdrawalPageSize {
		t.Fatalf("page size was not clamped: %d rows", len(huge.List))
	}
}

// A rejection of a request whose commission is already negative stays
// arithmetically exact instead of wrapping.
func TestRejectionRefundsCorrectlyWhenCommissionIsNegative(t *testing.T) {
	f := newFixture(t)
	withdrawal := f.seedUser(t, 7, -50, 400)

	logic := NewReviewWithdrawalLogic(context.Background(), f.svcCtx)
	if err := logic.ReviewWithdrawal(&types.ReviewWithdrawalRequest{Id: withdrawal.Id, Status: types.WithdrawalStatusRejected, Reason: "no"}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if got := f.commission(t, 7); got != 350 {
		t.Fatalf("commission = %d, want 350", got)
	}
}

func TestAddInt64DetectsOverflow(t *testing.T) {
	tests := []struct {
		a, b int64
		want int64
		ok   bool
	}{
		{a: 1, b: 2, want: 3, ok: true},
		{a: -50, b: 400, want: 350, ok: true},
		{a: 0, b: 0, want: 0, ok: true},
		{a: 1<<63 - 1, b: 1, ok: false},
		{a: -(1 << 63), b: -1, ok: false},
	}
	for _, tt := range tests {
		got, ok := addInt64(tt.a, tt.b)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("addInt64(%d, %d) = %d/%v, want %d/%v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}
