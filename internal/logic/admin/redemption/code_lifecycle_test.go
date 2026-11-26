package redemption

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestRedemptionCodeAdministrationPreservesUsageHistory(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	plan := client.Subscribe.Create().SetName("Membership").SaveX(ctx)
	create := NewCreateRedemptionCodeLogic(ctx, service)
	req := &types.CreateRedemptionCodeRequest{SubscribePlan: plan.ID, TotalCount: 4, UnitTime: "half_year", Quantity: 2}
	if err := create.CreateRedemptionCode(req); err == nil || client.RedemptionCode.Query().CountX(ctx) != 0 {
		t.Fatal("empty batch was allowed to create codes")
	}
	req.BatchCount = 2
	writes := 0
	client.RedemptionCode.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) {
				writes++
				if writes == 2 {
					return nil, errors.New("voucher storage unavailable")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	if err := create.CreateRedemptionCode(req); err == nil {
		t.Fatal("interrupted issue returned success")
	}
	if issued := client.RedemptionCode.Query().CountX(ctx); issued != 0 {
		t.Fatalf("interrupted batch published %d redeemable codes", issued)
	}
	if err := create.CreateRedemptionCode(req); err != nil {
		t.Fatal(err)
	}
	catalog := NewGetRedemptionCodeListLogic(ctx, service)
	query := &types.GetRedemptionCodeListRequest{Page: 1, Size: 10, SubscribePlan: plan.ID, UnitTime: "HalfYear"}
	created, err := catalog.GetRedemptionCodeList(query)
	if err != nil || created.Total != 2 || len(created.List) != 2 {
		t.Fatalf("generated codes = %+v, %v", created, err)
	}
	first, second := created.List[0], created.List[1]
	if first.Code == "" || second.Code == "" || first.Code == second.Code {
		t.Fatal("batch did not produce two distinct codes")
	}
	if first.TotalCount != 4 || first.Quantity != 2 || first.Status != 1 || first.UsedCount != 0 {
		t.Fatal("new code did not match the issued terms")
	}
	client.RedemptionCode.UpdateOneID(first.Id).SetUsedCount(2).SaveX(ctx)
	at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	claim := client.RedemptionRecord.Create().SetRedemptionCodeID(first.Id).SetUserID(7).SetSubscribeID(plan.ID).
		SetUnitTime("HalfYear").SetQuantity(2).SetRedeemedAt(at).SaveX(ctx)
	client.RedemptionRecord.Create().SetRedemptionCodeID(first.Id).SetUserID(8).SaveX(ctx)
	client.RedemptionRecord.Create().SetRedemptionCodeID(second.Id).SetUserID(7).SaveX(ctx)

	update := NewUpdateRedemptionCodeLogic(ctx, service)
	if err := update.UpdateRedemptionCode(&types.UpdateRedemptionCodeRequest{Id: first.Id, TotalCount: 1}); err == nil {
		t.Fatal("total allowance was reduced below previous claims")
	}
	if got := client.RedemptionCode.GetX(ctx, first.Id); got.TotalCount != 4 || got.UsedCount != 2 {
		t.Fatal("rejected allowance update changed usage or issued terms")
	}
	if err := update.UpdateRedemptionCode(&types.UpdateRedemptionCodeRequest{Id: first.Id, TotalCount: 5, UnitTime: "year", Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if err := NewToggleRedemptionCodeStatusLogic(ctx, service).ToggleRedemptionCodeStatus(&types.ToggleRedemptionCodeStatusRequest{Id: first.Id, Status: 0}); err != nil {
		t.Fatal(err)
	}
	updated, err := catalog.GetRedemptionCodeList(&types.GetRedemptionCodeListRequest{Page: 1, Size: 1, Code: first.Code, UnitTime: "Year"})
	if err != nil || updated.Total != 1 || len(updated.List) != 1 || updated.List[0].Id != first.Id || updated.List[0].TotalCount != 5 || updated.List[0].UsedCount != 2 || updated.List[0].Quantity != 1 || updated.List[0].Status != 0 {
		t.Fatal("catalog did not return the updated and disabled code")
	}
	if err := NewDeleteRedemptionCodeLogic(ctx, service).DeleteRedemptionCode(&types.DeleteRedemptionCodeRequest{Id: first.Id}); err != nil {
		t.Fatal(err)
	}
	remaining, err := catalog.GetRedemptionCodeList(query)
	if err != nil || remaining.Total != 1 || len(remaining.List) != 1 || remaining.List[0].Id != second.Id {
		t.Fatal("retiring the first code affected another code")
	}
	if err := NewBatchDeleteRedemptionCodeLogic(ctx, service).BatchDeleteRedemptionCode(&types.BatchDeleteRedemptionCodeRequest{Ids: []string{strconv.FormatInt(second.Id, 10)}}); err != nil {
		t.Fatal(err)
	}
	remaining, err = catalog.GetRedemptionCodeList(&types.GetRedemptionCodeListRequest{Page: 1, Size: 10})
	if err != nil || remaining.Total != 0 || len(remaining.List) != 0 {
		t.Fatal("retired codes remain in the active catalog")
	}
	history, err := NewGetRedemptionRecordListLogic(ctx, service).GetRedemptionRecordList(&types.GetRedemptionRecordListRequest{Page: 1, Size: 1, UserId: 7, CodeId: first.Id})
	if err != nil || history.Total != 1 || len(history.List) != 1 || history.List[0].Id != claim.ID || history.List[0].Quantity != 2 || history.List[0].RedeemedAt != at.Unix() {
		t.Fatalf("retired code usage history = %+v, %v", history, err)
	}
}
