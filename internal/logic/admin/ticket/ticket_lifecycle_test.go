package ticket

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modelticket "github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestAdminTicketReplyStatusAndSearchLifecycle(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	open := client.Ticket.Create().SetTitle("Cannot connect").SetDescription("VPN fails after update").SetUserID(7).SetStatus(modelticket.Pending).SaveX(ctx)
	other := client.Ticket.Create().SetTitle("Billing question").SetDescription("Invoice copy").SetUserID(8).SetStatus(modelticket.Waiting).SaveX(ctx)
	client.Ticket.Create().SetTitle("Old issue").SetUserID(7).SetStatus(modelticket.Closed).SaveX(ctx)

	list, err := NewGetTicketListLogic(ctx, service).GetTicketList(&types.GetTicketListRequest{Page: 1, Size: 10, Search: "VPN"})
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].Id != open.ID {
		t.Fatalf("search ticket list = %+v, %v", list, err)
	}
	defaultList, err := NewGetTicketListLogic(ctx, service).GetTicketList(&types.GetTicketListRequest{Page: 1, Size: 10})
	if err != nil || defaultList.Total != 2 {
		t.Fatalf("default ticket list included a closed ticket: %+v, %v", defaultList, err)
	}
	waiting := uint8(modelticket.Waiting)
	filtered, err := NewGetTicketListLogic(ctx, service).GetTicketList(&types.GetTicketListRequest{Page: 1, Size: 10, UserId: 8, Status: &waiting})
	if err != nil || filtered.Total != 1 || filtered.List[0].Id != other.ID {
		t.Fatalf("filtered ticket list = %+v, %v", filtered, err)
	}

	if err := NewCreateTicketFollowLogic(ctx, service).CreateTicketFollow(&types.CreateTicketFollowRequest{TicketId: open.ID, From: "Support", Type: modelticket.FollowText, Content: "Please try signing in again."}); err != nil {
		t.Fatal(err)
	}
	if got := client.Ticket.GetX(ctx, open.ID).Status; got != modelticket.Waiting {
		t.Fatalf("reply status = %d, want waiting", got)
	}
	detail, err := NewGetTicketLogic(ctx, service).GetTicket(&types.GetTicketRequest{Id: open.ID})
	if err != nil || len(detail.Follows) != 1 || detail.Follows[0].From != "Support" || detail.Follows[0].Content != "Please try signing in again." {
		t.Fatalf("ticket detail with reply = %+v, %v", detail, err)
	}

	invalidStatus := uint8(0)
	if err := NewUpdateTicketStatusLogic(ctx, service).UpdateTicketStatus(&types.UpdateTicketStatusRequest{Id: open.ID, Status: &invalidStatus}); err == nil {
		t.Fatal("invalid status was accepted")
	}
	closedStatus := uint8(modelticket.Closed)
	if err := NewUpdateTicketStatusLogic(ctx, service).UpdateTicketStatus(&types.UpdateTicketStatusRequest{Id: open.ID, Status: &closedStatus}); err != nil {
		t.Fatal(err)
	}
	if got := client.Ticket.GetX(ctx, open.ID).Status; got != modelticket.Closed {
		t.Fatalf("updated status = %d, want closed", got)
	}
	if err := NewCreateTicketFollowLogic(ctx, service).CreateTicketFollow(&types.CreateTicketFollowRequest{TicketId: open.ID, From: "Support", Type: modelticket.FollowText, Content: "This ticket is closed."}); err == nil {
		t.Fatal("reply to a closed ticket was accepted")
	}
	if count := client.TicketFollow.Query().Where().CountX(ctx); count != 1 {
		t.Fatalf("closed-ticket reply was persisted: %d follows", count)
	}
	if logs := client.SystemLog.Query().Where().AllX(ctx); len(logs) != 2 {
		t.Fatalf("reply and status change should be audited, got %+v", logs)
	}
	if err := NewUpdateTicketStatusLogic(ctx, service).UpdateTicketStatus(&types.UpdateTicketStatusRequest{}); err == nil {
		t.Fatal("empty status update was accepted")
	}
}
