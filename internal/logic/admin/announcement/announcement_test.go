package announcement

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	publicAnnouncement "github.com/perfect-panel/server/internal/logic/public/announcement"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestAnnouncementManagementAndPublicVisibility(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}

	create := NewCreateAnnouncementLogic(ctx, service)
	if err := create.CreateAnnouncement(&types.CreateAnnouncementRequest{Title: "Maintenance", Content: "Scheduled outage at midnight"}); err != nil {
		t.Fatal(err)
	}
	if err := create.CreateAnnouncement(&types.CreateAnnouncementRequest{Title: "Draft", Content: "Internal notes only"}); err != nil {
		t.Fatal(err)
	}
	all, err := NewGetAnnouncementListLogic(ctx, service).GetAnnouncementList(&types.GetAnnouncementListRequest{Page: 1, Size: 1})
	if err != nil || all.Total != 2 || len(all.List) != 1 {
		t.Fatalf("paginated announcements = %+v, %v", all, err)
	}
	if all.List[0].Title != "Maintenance" {
		t.Fatalf("default ordering = %+v", all.List)
	}

	announcement := client.Announcement.Query().Where().FirstX(ctx)
	visible, pinned, popup := true, true, false
	if err := NewUpdateAnnouncementLogic(ctx, service).UpdateAnnouncement(&types.UpdateAnnouncementRequest{
		Id: announcement.ID, Title: "Maintenance notice", Content: "Scheduled outage at midnight", Show: &visible, Pinned: &pinned, Popup: &popup,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := NewGetAnnouncementLogic(ctx, service).GetAnnouncement(&types.GetAnnouncementRequest{Id: announcement.ID})
	if err != nil || updated.Title != "Maintenance notice" || updated.Show == nil || !*updated.Show || updated.Pinned == nil || !*updated.Pinned || updated.Popup == nil || *updated.Popup {
		t.Fatalf("updated announcement = %+v, %v", updated, err)
	}

	filtered, err := NewGetAnnouncementListLogic(ctx, service).GetAnnouncementList(&types.GetAnnouncementListRequest{
		Page: 1, Size: 10, Show: &visible, Pinned: &pinned, Search: "outage",
	})
	if err != nil || filtered.Total != 1 || len(filtered.List) != 1 || filtered.List[0].Id != announcement.ID {
		t.Fatalf("admin filters = %+v, %v", filtered, err)
	}
	public, err := publicAnnouncement.NewQueryAnnouncementLogic(ctx, service).QueryAnnouncement(&types.QueryAnnouncementRequest{Page: 1, Size: 10})
	if err != nil || public.Total != 1 || len(public.List) != 1 || public.List[0].Id != announcement.ID {
		t.Fatalf("public visibility = %+v, %v", public, err)
	}
	pinnedOnly, err := publicAnnouncement.NewQueryAnnouncementLogic(ctx, service).QueryAnnouncement(&types.QueryAnnouncementRequest{Page: 1, Size: 10, Pinned: &pinned})
	if err != nil || pinnedOnly.Total != 1 || pinnedOnly.List[0].Id != announcement.ID {
		t.Fatalf("public pinned filter = %+v, %v", pinnedOnly, err)
	}
	if _, err := NewGetAnnouncementLogic(ctx, service).GetAnnouncement(&types.GetAnnouncementRequest{Id: 999}); err == nil {
		t.Fatal("missing announcement lookup succeeded")
	}
	if err := NewDeleteAnnouncementLogic(ctx, service).DeleteAnnouncement(&types.DeleteAnnouncementRequest{Id: announcement.ID}); err != nil {
		t.Fatal(err)
	}
	remaining, err := NewGetAnnouncementListLogic(ctx, service).GetAnnouncementList(&types.GetAnnouncementListRequest{Page: 1, Size: 10})
	if err != nil || remaining.Total != 1 || len(remaining.List) != 1 || remaining.List[0].Title != "Draft" {
		t.Fatalf("announcements after deletion = %+v, %v", remaining, err)
	}
}
