package group

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
)

func TestPreviewUsesConfiguredGroupingAndKeepsDirectNodes(t *testing.T) {
	ctx, db, fixture := newRecalculationFixture(t)
	service := fixture.svcCtx
	config := NewGetGroupConfigLogic(ctx, service)
	defaults, err := config.GetGroupConfig(&types.GetGroupConfigRequest{})
	if err != nil || defaults.Enabled || defaults.Mode != "average" || defaults.State.State != "idle" {
		t.Fatalf("default grouping configuration = %+v, %v", defaults, err)
	}
	assigned := db.NodeGroup.Create().SetName("Assigned").SaveX(ctx)
	defaultGroup := db.NodeGroup.Create().SetName("Default").SaveX(ctx)
	member := db.User.Create().SetPassword("fixture").SaveX(ctx)
	assignedNode := db.Node.Create().SetName("Assigned node").SetNodeGroupIds([]int64{assigned.ID}).SaveX(ctx)
	tagged := db.Node.Create().SetName("Tagged node").SetTags("app").SetNodeGroupIds([]int64{defaultGroup.ID}).SaveX(ctx)
	public := db.Node.Create().SetName("Public node").SaveX(ctx)
	direct := db.Node.Create().SetName("Direct node").SetNodeGroupIds([]int64{defaultGroup.ID}).SaveX(ctx)
	hidden := db.Node.Create().SetName("Hidden node").SetTags("app").SetIsHidden(true).SaveX(ctx)
	db.Node.Create().SetName("Disabled node").SetTags("app").SetEnabled(false).SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Standard").SetNodeGroupID(defaultGroup.ID).SetNodeTags("app").
		SetNodes(fmt.Sprintf("%d,%d,%d", direct.ID, direct.ID, hidden.ID)).SaveX(ctx)
	membership := db.UserSubscribe.Create().SetUserID(member.ID).SetSubscribeID(plan.ID).SetOrderID(1).SetNodeGroupID(assigned.ID).SetStatus(1).SaveX(ctx)

	preview := NewPreviewUserNodesLogic(ctx, service)
	type section struct {
		id   int64
		name string
	}
	check := func(want map[section][]int64) {
		t.Helper()
		got, err := preview.PreviewUserNodes(&types.PreviewUserNodesRequest{UserId: member.ID})
		if err != nil || got.UserId != member.ID || len(got.NodeGroups) != len(want) {
			t.Fatalf("preview = %+v, %v", got, err)
		}
		remaining := maps.Clone(want)
		for _, group := range got.NodeGroups {
			key := section{group.Id, group.Name}
			var ids []int64
			for _, item := range group.Nodes {
				ids = append(ids, item.Id)
			}
			if expected, found := remaining[key]; !found || !slices.Equal(ids, expected) {
				t.Fatalf("preview group %v contains %v, want %v", key, ids, expected)
			}
			delete(remaining, key)
		}
	}
	var offline string
	fault := interruptGroupCatalog(db, &offline)
	checkOutages := func(sources ...string) {
		t.Helper()
		for _, offline = range sources {
			result, err := preview.PreviewUserNodes(&types.PreviewUserNodesRequest{UserId: member.ID})
			if result != nil || !errors.Is(err, fault) {
				t.Fatalf("preview with %s unavailable = %+v, %v", offline, result, err)
			}
		}
		offline = ""
	}
	checkOutages(ent.TypeUserSubscribe, ent.TypeSubscribe, ent.TypeNode)
	check(map[section][]int64{{0, "app"}: {tagged.ID}, {-1, ""}: {direct.ID}})
	update := NewUpdateGroupConfigLogic(ctx, service)
	if err := update.UpdateGroupConfig(&types.UpdateGroupConfigRequest{Enabled: true, Mode: "unsupported"}); err == nil {
		t.Fatal("invalid grouping mode was accepted")
	}
	if count := db.System.Query().CountX(ctx); count != 0 {
		t.Fatalf("invalid grouping mode persisted %d settings", count)
	}
	if err := update.UpdateGroupConfig(&types.UpdateGroupConfigRequest{Enabled: true, Mode: "traffic", Config: map[string]interface{}{
		"traffic_config": map[string]interface{}{"threshold": 12},
	}}); err != nil {
		t.Fatal(err)
	}
	settings, err := config.GetGroupConfig(&types.GetGroupConfigRequest{})
	if err != nil || !settings.Enabled || settings.Mode != "traffic" {
		t.Fatalf("stored grouping settings = %+v, %v", settings, err)
	}
	if cfg, ok := settings.Config["traffic_config"].(map[string]interface{}); !ok || cfg["threshold"] != float64(12) {
		t.Fatalf("stored traffic config = %+v", settings.Config)
	}
	checkOutages(ent.TypeNode, ent.TypeNodeGroup)
	check(map[section][]int64{{assigned.ID, assigned.Name}: {assignedNode.ID}, {0, ""}: {public.ID}, {-1, ""}: {direct.ID}})
	db.UserSubscribe.UpdateOneID(membership.ID).SetNodeGroupID(0).ExecX(ctx)
	check(map[section][]int64{{defaultGroup.ID, defaultGroup.Name}: {tagged.ID, direct.ID}, {0, ""}: {public.ID}, {-1, ""}: {direct.ID}})
	db.Subscribe.UpdateOneID(plan.ID).SetNodeGroupID(0).SetNodeGroupIds([]int64{assigned.ID, defaultGroup.ID}).ExecX(ctx)
	check(map[section][]int64{{assigned.ID, assigned.Name}: {assignedNode.ID}, {0, ""}: {public.ID}, {-1, ""}: {direct.ID}})
	db.UserSubscribe.UpdateOneID(membership.ID).SetStatus(3).ExecX(ctx)
	check(map[section][]int64{})
}
