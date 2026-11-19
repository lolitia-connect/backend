package ticket

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

var ticketFixtureID atomic.Uint64

func ticketClient(t *testing.T) *ent.Client {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:ticket-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", ticketFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestReplyOwnershipAndClosedTransitions(t *testing.T) {
	client := ticketClient(t)
	ctx := context.Background()
	current := client.Ticket.Create().SetUserID(7).SetStatus(Closed).SaveX(ctx)
	model := NewModel(client)
	staff := &Follow{TicketId: current.ID, From: "System", Type: FollowText, Content: "answer"}
	if err := model.Reply(ctx, staff, 0); err == nil {
		t.Fatal("staff replied to a closed ticket")
	}
	if client.TicketFollow.Query().CountX(ctx) != 0 {
		t.Fatal("rejected reply was stored")
	}
	owner := &Follow{TicketId: current.ID, From: "System", Content: "still broken"}
	if err := model.Reply(ctx, owner, 8); err == nil {
		t.Fatal("another account replied to the ticket")
	}
	if err := model.Reply(ctx, owner, 7); err != nil {
		t.Fatal(err)
	}
	stored := client.TicketFollow.GetX(ctx, owner.Id)
	if stored.From != "User" || stored.Type != FollowText || client.Ticket.GetX(ctx, current.ID).Status != Pending {
		t.Fatalf("owner reply did not reopen as User: %+v", stored)
	}
	if err := model.Reply(ctx, staff, 0); err != nil {
		t.Fatal(err)
	}
	if client.Ticket.GetX(ctx, current.ID).Status != Waiting {
		t.Fatal("staff reply did not transfer the ticket to Waiting")
	}
}

func TestReplyRollsBackWhenStatusWriteFails(t *testing.T) {
	client := ticketClient(t)
	ctx := context.Background()
	current := client.Ticket.Create().SetUserID(7).SetStatus(Pending).SaveX(ctx)
	if err := client.Driver().Exec(ctx, "CREATE TRIGGER reject_ticket_update BEFORE UPDATE ON ticket BEGIN SELECT RAISE(ABORT, 'status write failed'); END", []any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := NewModel(client).Reply(ctx, &Follow{TicketId: current.ID, From: "System", Type: FollowText, Content: "answer"}, 0); err == nil {
		t.Fatal("status write failure ignored")
	}
	if client.TicketFollow.Query().CountX(ctx) != 0 || client.Ticket.GetX(ctx, current.ID).Status != Pending {
		t.Fatal("failed reply left a partial change")
	}
}

func TestTicketInputBounds(t *testing.T) {
	if err := ValidateTicket(&Ticket{Title: strings.Repeat("t", MaxTitleLength), Description: strings.Repeat("d", MaxDescriptionLength)}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*Ticket{nil, {}, {Title: strings.Repeat("t", MaxTitleLength+1)}, {Title: "help", Description: strings.Repeat("d", MaxDescriptionLength+1)}} {
		if err := ValidateTicket(invalid); err == nil {
			t.Fatal("invalid ticket accepted")
		}
	}
	valid := &Follow{TicketId: 1, From: "User", Type: FollowText, Content: strings.Repeat("c", MaxFollowLength)}
	if err := ValidateFollow(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*Follow{nil, {}, {TicketId: 1, From: "User", Type: 7, Content: "answer"}, {TicketId: 1, From: "User", Type: FollowText, Content: valid.Content + "x"}} {
		if err := ValidateFollow(invalid); err == nil {
			t.Fatal("invalid reply accepted")
		}
	}
}

func TestTicketImageReferences(t *testing.T) {
	for _, content := range []string{"data:image/webp;base64,UklGRg==", "data:image/png;base64,iVBORw==", "DATA:IMAGE/JPEG;BASE64,/9g=", "data:image/gif;base64,R0lGOA==", "https://images.example.test/ticket.png", "http://images.example.test/ticket"} {
		if err := ValidateFollow(&Follow{TicketId: 1, From: "User", Type: FollowImage, Content: content}); err != nil {
			t.Fatalf("supported image rejected: %q: %v", content, err)
		}
	}
	for _, content := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "data:text/html,<script>", "javascript:alert(1)", "/image.png", "//images.example.test/image.png", "https:///image.png", "data:image/png;base64,", "<svg onload='alert(1)'/>"} {
		if err := ValidateFollow(&Follow{TicketId: 1, From: "User", Type: FollowImage, Content: content}); err == nil {
			t.Fatalf("unsafe image accepted: %q", content)
		}
	}
}

func TestTicketStatusOwnershipAndPagination(t *testing.T) {
	client := ticketClient(t)
	ctx := context.Background()
	current := client.Ticket.Create().SetUserID(7).SetStatus(Pending).SaveX(ctx)
	model := NewModel(client)
	if err := model.UpdateTicketStatus(ctx, current.ID, 8, Closed); err == nil {
		t.Fatal("another owner changed the status")
	}
	if err := model.UpdateTicketStatus(ctx, current.ID, 7, Waiting); err == nil {
		t.Fatal("owner set a staff status")
	}
	if err := model.UpdateTicketStatus(ctx, current.ID+1, 0, Closed); err == nil {
		t.Fatal("missing ticket was silently updated")
	}
	if err := model.UpdateTicketStatus(ctx, current.ID, 7, Closed); err != nil {
		t.Fatal(err)
	}
	status := uint8(Closed)
	if total, rows, err := model.QueryTicketList(ctx, -1, -1, 7, &status, ""); err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("invalid default pagination: %d, %v, %v", total, rows, err)
	}
}

func TestTicketWriteQuotas(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer client.Close()
	ctx := context.Background()
	for _, reply := range []bool{false, true} {
		quota := 5
		if reply {
			quota = 30
		}
		for count := 0; count < quota; count++ {
			if err := AllowWrite(ctx, client, 7, reply); err != nil {
				t.Fatal(err)
			}
		}
		if err := AllowWrite(ctx, client, 7, reply); err == nil {
			t.Fatal("quota was not enforced")
		}
		if err := AllowWrite(ctx, client, 8, reply); err != nil {
			t.Fatal("another account shared the quota")
		}
	}
	mini.FastForward(time.Hour)
	if err := AllowWrite(ctx, client, 7, false); err != nil {
		t.Fatal("quota did not expire")
	}
	if err := AllowWrite(ctx, nil, 7, false); err != nil {
		t.Fatal("optional quota rejected nil Redis")
	}
}
