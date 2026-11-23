package group

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func newRecalculationFixture(t *testing.T) (context.Context, *ent.Client, *RecalculateGroupLogic) {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	store := repository.NewStore(client, nil)
	logic := NewRecalculateGroupLogic(ctx, &svc.ServiceContext{Store: store})
	return ctx, client, logic
}

func TestRecalculateAverageGroupingRotatesEligibleGroups(t *testing.T) {
	ctx, client, logic := newRecalculationFixture(t)

	first := client.NodeGroup.Create().SetName("North").SetForCalculation(true).SaveX(ctx)
	second := client.NodeGroup.Create().SetName("South").SetForCalculation(true).SaveX(ctx)
	excluded := client.NodeGroup.Create().SetName("Manual").SetForCalculation(false).SaveX(ctx)
	eligiblePlan := client.Subscribe.Create().SetName("eligible").SetNodeGroupIds([]int64{first.ID, second.ID, excluded.ID}).SaveX(ctx)
	manualPlan := client.Subscribe.Create().SetName("manual").SetNodeGroupIds([]int64{excluded.ID}).SaveX(ctx)
	newUserSubscribe := func(email string, planID, currentGroup int64, locked bool) int64 {
		t.Helper()
		account := client.User.Create().SetPassword("test").SaveX(ctx)
		client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier(email).SaveX(ctx)
		return client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(planID).SetNodeGroupID(currentGroup).SetGroupLocked(locked).SetStatus(1).SaveX(ctx).ID
	}
	firstSub := newUserSubscribe("first@example.test", eligiblePlan.ID, 99, false)
	secondSub := newUserSubscribe("second@example.test", eligiblePlan.ID, 99, false)
	manualSub := newUserSubscribe("manual@example.test", manualPlan.ID, 88, false)
	lockedSub := newUserSubscribe("locked@example.test", eligiblePlan.ID, 77, true)

	if err := logic.RecalculateGroup(&types.RecalculateGroupRequest{Mode: "average"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   int64
		want int64
	}{{firstSub, first.ID}, {secondSub, second.ID}, {manualSub, 0}, {lockedSub, 77}} {
		got := client.UserSubscribe.GetX(ctx, tc.id)
		if got.NodeGroupID != tc.want {
			t.Errorf("user subscribe %d node group = %d, want %d", tc.id, got.NodeGroupID, tc.want)
		}
	}
	history := client.GroupHistory.Query().OnlyX(ctx)
	if history.GroupMode != "average" || history.TriggerType != "manual" || history.State != "completed" || history.TotalUsers != 3 || history.SuccessCount != 3 || history.FailedCount != 0 || history.StartTime == nil || history.EndTime == nil {
		t.Fatalf("unexpected recalculation history: %+v", history)
	}
	details := client.GroupHistoryDetail.Query().AllX(ctx)
	if len(details) != 2 {
		t.Fatalf("history has %d group details, want 2: %+v", len(details), details)
	}
	counts := map[int64]int{}
	for _, detail := range details {
		counts[detail.NodeGroupID] = detail.UserCount
	}
	if counts[first.ID] != 1 || counts[second.ID] != 1 {
		t.Fatalf("history group assignment counts = %v", counts)
	}

	if err := logic.RecalculateGroup(&types.RecalculateGroupRequest{Mode: "invalid"}); err == nil {
		t.Fatal("invalid grouping mode was accepted")
	}
	if count := client.GroupHistory.Query().CountX(ctx); count != 1 {
		t.Fatalf("invalid mode created history: %d records", count)
	}
}

func TestNodeGroupCreateAndUpdateEnforceTypeExpiryAndTrafficRanges(t *testing.T) {
	ctx, client, _ := newRecalculationFixture(t)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	create := NewCreateNodeGroupLogic(ctx, service)
	firstMin, firstMax := int64(0), int64(10)
	if err := create.CreateNodeGroup(&types.CreateNodeGroupRequest{Name: "low", Type: " COMMON ", MinTrafficGB: &firstMin, MaxTrafficGB: &firstMax}); err != nil {
		t.Fatal(err)
	}
	secondMin, secondMax := int64(10), int64(20)
	if err := create.CreateNodeGroup(&types.CreateNodeGroupRequest{Name: "middle", MinTrafficGB: &secondMin, MaxTrafficGB: &secondMax}); err != nil {
		t.Fatal(err)
	}
	overlapMin, overlapMax := int64(5), int64(15)
	if err := NewUpdateNodeGroupLogic(ctx, service).UpdateNodeGroup(&types.UpdateNodeGroupRequest{Id: 2, MinTrafficGB: &overlapMin, MaxTrafficGB: &overlapMax}); err == nil {
		t.Fatal("overlapping traffic range was accepted")
	}
	validMin, validMax := int64(10), int64(20)
	updatedSort := 9
	if err := NewUpdateNodeGroupLogic(ctx, service).UpdateNodeGroup(&types.UpdateNodeGroupRequest{
		Id: 2, Name: "high", Type: "app", Sort: &updatedSort, MinTrafficGB: &validMin, MaxTrafficGB: &validMax,
	}); err != nil {
		t.Fatal(err)
	}
	updated := client.NodeGroup.GetX(ctx, 2)
	if updated.Name != "high" || updated.Type != "app" || updated.Sort != 9 || updated.MinTrafficGB == nil || *updated.MinTrafficGB != 10 {
		t.Fatalf("updated node group = %+v", updated)
	}
	if err := NewUpdateNodeGroupLogic(ctx, service).UpdateNodeGroup(&types.UpdateNodeGroupRequest{Id: 2, Type: "unknown"}); err == nil {
		t.Fatal("unknown node group type was accepted")
	}
	if err := NewUpdateNodeGroupLogic(ctx, service).UpdateNodeGroup(&types.UpdateNodeGroupRequest{Id: 999}); err == nil {
		t.Fatal("missing node group update succeeded")
	}

	expired := true
	if err := create.CreateNodeGroup(&types.CreateNodeGroupRequest{Name: "expired", IsExpiredGroup: &expired}); err != nil {
		t.Fatal(err)
	}
	groups := client.NodeGroup.Query().AllX(ctx)
	var expiredGroup *ent.NodeGroup
	for _, candidate := range groups {
		if candidate.IsExpiredGroup {
			expiredGroup = candidate
		}
	}
	if expiredGroup == nil || expiredGroup.ForCalculation || expiredGroup.ExpiredDaysLimit != 7 {
		t.Fatalf("expired node group defaults = %+v", expiredGroup)
	}
	if err := create.CreateNodeGroup(&types.CreateNodeGroupRequest{Name: "second expired", IsExpiredGroup: &expired}); err == nil {
		t.Fatal("a second expired group was accepted")
	}
	if err := create.CreateNodeGroup(&types.CreateNodeGroupRequest{Name: "invalid", Type: "unexpected"}); err == nil {
		t.Fatal("invalid node group type was accepted")
	}
	list, err := NewGetNodeGroupListLogic(ctx, service).GetNodeGroupList(&types.GetNodeGroupListRequest{Page: 1, Size: 10})
	if err != nil || list.Total != 3 || len(list.List) != 3 {
		t.Fatalf("node group list = %+v, %v", list, err)
	}
}

func addGroupingSubscription(t *testing.T, ctx context.Context, client *ent.Client, planID int64, status uint8, locked bool, usedTrafficGB float64) int64 {
	t.Helper()
	account := client.User.Create().SetPassword("test").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier(fmt.Sprintf("user-%d@example.test", account.ID)).SaveX(ctx)
	return client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(account.ID).SetSubscribeID(planID).
		SetNodeGroupID(99).SetGroupLocked(locked).SetStatus(status).
		SetUpload(int64(usedTrafficGB * 1024 * 1024 * 1024)).SaveX(ctx).ID
}

func TestRecalculateSubscribeGroupingFiltersGroupsAndClearsExpired(t *testing.T) {
	ctx, client, logic := newRecalculationFixture(t)
	eligible := client.NodeGroup.Create().SetName("eligible").SetForCalculation(true).SaveX(ctx)
	excluded := client.NodeGroup.Create().SetName("excluded").SetForCalculation(false).SaveX(ctx)
	matchingPlan := client.Subscribe.Create().SetName("matching").SetNodeGroupIds([]int64{excluded.ID, eligible.ID}).SaveX(ctx)
	excludedPlan := client.Subscribe.Create().SetName("excluded-only").SetNodeGroupIds([]int64{excluded.ID}).SaveX(ctx)
	matching := addGroupingSubscription(t, ctx, client, matchingPlan.ID, 1, false, 0)
	noCalculationGroup := addGroupingSubscription(t, ctx, client, excludedPlan.ID, 1, false, 0)
	expired := addGroupingSubscription(t, ctx, client, matchingPlan.ID, 3, false, 0)
	locked := addGroupingSubscription(t, ctx, client, matchingPlan.ID, 1, true, 0)

	if err := logic.RecalculateGroup(&types.RecalculateGroupRequest{Mode: "subscribe", TriggerType: "scheduled"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, want int64 }{{matching, eligible.ID}, {noCalculationGroup, 0}, {expired, 0}, {locked, 99}} {
		if got := client.UserSubscribe.GetX(ctx, tc.id).NodeGroupID; got != tc.want {
			t.Errorf("subscription %d group = %d, want %d", tc.id, got, tc.want)
		}
	}
	history := client.GroupHistory.Query().OnlyX(ctx)
	if history.GroupMode != "subscribe" || history.TriggerType != "scheduled" || history.State != "completed" || history.TotalUsers != 2 {
		t.Fatalf("unexpected subscribe history: %+v", history)
	}
}

func TestRecalculateTrafficGroupingUsesLeftClosedRightOpenRanges(t *testing.T) {
	ctx, client, logic := newRecalculationFixture(t)
	low := client.NodeGroup.Create().SetName("low").SetForCalculation(true).SetMinTrafficGB(0).SetMaxTrafficGB(10).SaveX(ctx)
	high := client.NodeGroup.Create().SetName("high").SetForCalculation(true).SetMinTrafficGB(10).SetMaxTrafficGB(20).SaveX(ctx)
	client.NodeGroup.Create().SetName("unbounded").SetForCalculation(true).SetMinTrafficGB(20).SaveX(ctx)
	plan := client.Subscribe.Create().SetName("traffic plans").SaveX(ctx)
	lowUser := addGroupingSubscription(t, ctx, client, plan.ID, 1, false, 5)
	boundaryUser := addGroupingSubscription(t, ctx, client, plan.ID, 1, false, 10)
	upperBoundaryUser := addGroupingSubscription(t, ctx, client, plan.ID, 1, false, 20)
	lockedUser := addGroupingSubscription(t, ctx, client, plan.ID, 1, true, 15)

	if err := logic.RecalculateGroup(&types.RecalculateGroupRequest{Mode: "traffic"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, want int64 }{{lowUser, low.ID}, {boundaryUser, high.ID}, {upperBoundaryUser, 0}, {lockedUser, 99}} {
		if got := client.UserSubscribe.GetX(ctx, tc.id).NodeGroupID; got != tc.want {
			t.Errorf("subscription %d group = %d, want %d", tc.id, got, tc.want)
		}
	}
	if got := client.GroupHistory.Query().OnlyX(ctx); got.State != "completed" || got.TotalUsers != 3 {
		t.Fatalf("unexpected traffic history: %+v", got)
	}
	if details := client.GroupHistoryDetail.Query().AllX(ctx); len(details) != 2 {
		t.Fatalf("traffic history details = %d, want only assigned groups", len(details))
	}
}
