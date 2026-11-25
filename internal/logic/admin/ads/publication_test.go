package ads

import (
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestAdPublicationStatusAndAdminSearch(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	end := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	create := NewCreateAdsLogic(ctx, service)
	if err := create.CreateAds(&types.CreateAdsRequest{Title: "Winter offer", Content: "Annual plan", Type: "text", TargetURL: "https://example.test/annual", StartTime: start, EndTime: end, Status: 1}); err != nil {
		t.Fatal(err)
	}
	if err := create.CreateAds(&types.CreateAdsRequest{Title: "Spring offer", Content: "Annual plan", Type: "text", StartTime: start, EndTime: end}); err != nil {
		t.Fatal(err)
	}
	active := 1
	results, err := NewGetAdsListLogic(ctx, service).GetAdsList(&types.GetAdsListRequest{Page: 1, Size: 1, Status: &active, Search: "Annual"})
	if err != nil || results.Total != 1 || len(results.List) != 1 || results.List[0].Title != "Winter offer" {
		t.Fatalf("filtered campaigns = %+v, %v", results, err)
	}
	id := int64(results.List[0].Id)
	public := common.NewGetAdsLogic(ctx, service)
	visible, err := public.GetAds(&types.GetAdsRequest{})
	if err != nil || len(visible.List) != 1 || int64(visible.List[0].Id) != id || visible.List[0].TargetURL != "https://example.test/annual" {
		t.Fatalf("published campaigns = %+v, %v", visible, err)
	}
	update := &types.UpdateAdsRequest{Id: id, Title: "Winter offer ended", Content: "Archived", Type: "text", TargetURL: "https://example.test/archive", StartTime: start, EndTime: end, Status: 0}
	if err := NewUpdateAdsLogic(ctx, service).UpdateAds(update); err != nil {
		t.Fatal(err)
	}
	details := NewGetAdsDetailLogic(ctx, service)
	ad, err := details.GetAdsDetail(&types.GetAdsDetailRequest{Id: id})
	if err != nil || ad.Title != update.Title || ad.Status != 0 || ad.TargetURL != update.TargetURL || ad.StartTime != start || ad.EndTime != end {
		t.Fatalf("updated campaign = %+v, %v", ad, err)
	}
	visible, err = public.GetAds(&types.GetAdsRequest{})
	if err != nil || len(visible.List) != 0 {
		t.Fatalf("disabled campaign still published: %+v, %v", visible, err)
	}
	if err := NewDeleteAdsLogic(ctx, service).DeleteAds(&types.DeleteAdsRequest{Id: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := details.GetAdsDetail(&types.GetAdsDetailRequest{Id: id}); err == nil {
		t.Fatal("deleted campaign still has a detail response")
	}
	if ad := client.Ads.Query().OnlyX(ctx); ad.Title != "Spring offer" {
		t.Fatalf("wrong campaign was retained: %+v", ad)
	}
}
