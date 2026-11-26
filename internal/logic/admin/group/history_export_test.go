package group

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGroupExportRecoversFromInterruptedGroupLookups(t *testing.T) {
	ctx, db, fixture := newRecalculationFixture(t)
	active := db.NodeGroup.Create().SetName("Active region").SaveX(ctx)
	retired := db.NodeGroup.Create().SetName("Retired region").SaveX(ctx)
	member := db.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Regional pass").SaveX(ctx)
	history := db.GroupHistory.Create().SetGroupMode("average").SetTriggerType("manual").SetState("completed").SaveX(ctx)
	for _, group := range []*ent.NodeGroup{active, retired} {
		db.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(group.ID).SetSubscribeID(plan.ID).SetNodeGroupID(group.ID).SaveX(ctx)
		db.GroupHistoryDetail.Create().SetHistoryID(history.ID).SetNodeGroupID(group.ID).SetUserCount(1).
			SetUserData(fmt.Sprintf(`[{"id":%d}]`, member.ID)).SaveX(ctx)
	}
	db.NodeGroup.DeleteOneID(retired.ID).ExecX(ctx)
	unavailable := errors.New("group catalog temporarily unavailable")
	var offline bool
	db.NodeGroup.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if offline {
				return nil, unavailable
			}
			return next.Query(ctx, q)
		})
	}))
	export := NewExportGroupResultLogic(ctx, fixture.svcCtx)
	activeRow := []string{fmt.Sprint(member.ID), fmt.Sprint(active.ID), active.Name}
	for _, tc := range []struct {
		name string
		id   *int64
		want [][]string
	}{
		{"current", nil, [][]string{activeRow}},
		{"historical", &history.ID, [][]string{activeRow, {fmt.Sprint(member.ID), fmt.Sprint(retired.ID), ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &types.ExportGroupResultRequest{HistoryId: tc.id}
			offline = true
			data, _, err := export.ExportGroupResult(req)
			require.ErrorIs(t, err, unavailable)
			require.Empty(t, data)
			offline = false
			data, _, err = export.ExportGroupResult(req)
			require.NoError(t, err)
			rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff"))).ReadAll()
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			require.ElementsMatch(t, tc.want, rows[1:])
		})
	}
}

func TestGroupingHistoryAndExportReflectPersistedAssignments(t *testing.T) {
	ctx, db, fixture := newRecalculationFixture(t)
	service := fixture.svcCtx
	north := db.NodeGroup.Create().SetName("North, primary").SaveX(ctx)
	south := db.NodeGroup.Create().SetName("South").SaveX(ctx)
	member := db.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	for id, groupID := range []int64{north.ID, north.ID, south.ID} {
		db.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(int64(id + 1)).SetSubscribeID(plan.ID).SetNodeGroupID(groupID).SaveX(ctx)
	}
	start := time.Date(2026, 3, 7, 11, 0, 0, 0, time.UTC)
	history := db.GroupHistory.Create().SetGroupMode("traffic").SetTriggerType("scheduled").SetState("completed").
		SetTotalUsers(1).SetSuccessCount(1).SetStartTime(start).SetEndTime(start.Add(time.Minute)).SaveX(ctx)
	db.GroupHistory.Create().SetGroupMode("traffic").SetTriggerType("manual").SetState("completed").SaveX(ctx)
	db.GroupHistoryDetail.Create().SetHistoryID(history.ID).SetNodeGroupID(north.ID).SetUserCount(1).
		SetUserData(fmt.Sprintf(`[{"id":%d}]`, member.ID)).SaveX(ctx)
	db.System.Create().SetCategory("group").SetKey("group.traffic_config").SetValue(`{"group_by":"usage"}`).SetType("json").SetDesc("fixture").SaveX(ctx)

	list, err := NewGetGroupHistoryLogic(ctx, service).GetGroupHistory(&types.GetGroupHistoryRequest{Page: 1, Size: 1, GroupMode: "traffic", TriggerType: "scheduled"})
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].Id != history.ID || list.List[0].SuccessCount != 1 || list.List[0].StartTime == nil || *list.List[0].StartTime != start.Unix() {
		t.Fatalf("scheduled grouping history = %+v, %v", list, err)
	}
	detail, err := NewGetGroupHistoryDetailLogic(ctx, service).GetGroupHistoryDetail(&types.GetGroupHistoryDetailRequest{Id: history.ID})
	if err != nil || detail.Id != history.ID || detail.TotalUsers != 1 {
		t.Fatalf("grouping history detail = %+v, %v", detail, err)
	}
	config, ok := detail.ConfigSnapshot["config"].(map[string]interface{})
	if !ok || config["group_by"] != "usage" {
		t.Fatalf("grouping configuration snapshot = %+v", detail.ConfigSnapshot)
	}

	for _, tc := range []struct {
		request types.ExportGroupResultRequest
		groups  []string
	}{
		{types.ExportGroupResultRequest{HistoryId: &history.ID}, []string{north.Name}},
		{types.ExportGroupResultRequest{}, []string{north.Name, south.Name}},
	} {
		data, _, err := NewExportGroupResultLogic(ctx, service).ExportGroupResult(&tc.request)
		if err != nil {
			t.Fatal(err)
		}
		csvText, found := strings.CutPrefix(string(data), "\ufeff")
		if !found {
			t.Fatal("export omitted the UTF-8 byte order mark")
		}
		records, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
		if err != nil || len(records) != len(tc.groups)+1 {
			t.Fatalf("group export = %v, %v", records, err)
		}
		var names []string
		for _, fields := range records[1:] {
			if len(fields) != 3 || fields[0] != fmt.Sprint(member.ID) {
				t.Fatalf("unexpected exported assignment: %v", fields)
			}
			names = append(names, fields[2])
		}
		slices.Sort(names)
		if !slices.Equal(names, tc.groups) {
			t.Fatalf("exported groups = %v, want %v", names, tc.groups)
		}
	}
}
