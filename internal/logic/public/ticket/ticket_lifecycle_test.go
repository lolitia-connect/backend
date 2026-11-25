package ticket

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modelticket "github.com/perfect-panel/server/internal/model/ticket"
	modeluser "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestCustomerTicketLifecycle(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	redisClient := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, redisClient), Redis: redisClient}
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &modeluser.User{Id: account.ID})
	create := NewCreateUserTicketLogic(ownerCtx, service)
	if err := create.CreateUserTicket(&types.CreateUserTicketRequest{Description: "Missing title"}); err == nil {
		t.Fatal("a ticket without a title was accepted")
	}
	if err := create.CreateUserTicket(&types.CreateUserTicketRequest{Title: "Connection issue", Description: "My router cannot connect"}); err != nil {
		t.Fatal(err)
	}
	initial := client.Ticket.Query().OnlyX(ctx)
	if initial.UserID != account.ID || initial.Status != modelticket.Pending {
		t.Fatalf("new ticket = %+v", initial)
	}

	if err := service.Store.Ticket().Reply(ctx, &modelticket.Follow{TicketId: initial.ID, From: "Support", Type: modelticket.FollowText, Content: "Please check the date and time."}, 0); err != nil {
		t.Fatal(err)
	}
	current := client.Ticket.GetX(ctx, initial.ID)
	if current.Status != modelticket.Waiting {
		t.Fatalf("after support reply status = %d", current.Status)
	}
	follow := NewCreateUserTicketFollowLogic(ownerCtx, service)
	if err := follow.CreateUserTicketFollow(&types.CreateUserTicketFollowRequest{TicketId: initial.ID, Type: 0, Content: "I corrected the time. The router connects now."}); err != nil {
		t.Fatal(err)
	}
	detail, err := NewGetUserTicketDetailsLogic(ownerCtx, service).GetUserTicketDetails(&types.GetUserTicketDetailRequest{Id: initial.ID})
	if err != nil || detail.Status != modelticket.Pending || len(detail.Follows) != 2 {
		t.Fatalf("ticket after customer reply = %+v, %v", detail, err)
	}
	if message := detail.Follows[1]; message.From != "User" || message.Type != modelticket.FollowText || message.Content != "I corrected the time. The router connects now." {
		t.Fatalf("customer reply = %+v", message)
	}
	closeTicket := NewUpdateUserTicketStatusLogic(ownerCtx, service)
	waiting := uint8(modelticket.Waiting)
	if err := closeTicket.UpdateUserTicketStatus(&types.UpdateUserTicketStatusRequest{Id: initial.ID, Status: &waiting}); err == nil {
		t.Fatal("customer was permitted to set the support waiting state")
	}
	closed := uint8(modelticket.Closed)
	if err := closeTicket.UpdateUserTicketStatus(&types.UpdateUserTicketStatusRequest{Id: initial.ID, Status: &closed}); err != nil {
		t.Fatal(err)
	}
	list := NewGetUserTicketListLogic(ownerCtx, service)
	open, err := list.GetUserTicketList(&types.GetUserTicketListRequest{Page: 1, Size: 10})
	if err != nil || open.Total != 0 || len(open.List) != 0 {
		t.Fatalf("open ticket view includes closed ticket: %+v, %v", open, err)
	}
	archived, err := list.GetUserTicketList(&types.GetUserTicketListRequest{Status: &closed, Search: "router", Page: 1, Size: 10})
	if err != nil || archived.Total != 1 || len(archived.List) != 1 || archived.List[0].Id != initial.ID {
		t.Fatalf("closed ticket search = %+v, %v", archived, err)
	}
}
