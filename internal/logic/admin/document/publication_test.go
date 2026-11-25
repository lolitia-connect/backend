package document

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	publicdoc "github.com/perfect-panel/server/internal/logic/public/document"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	_ "modernc.org/sqlite"
)

func TestDocumentPublicationSearchAndSubscriberContent(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	published, draft := true, false
	create := NewCreateDocumentLogic(ctx, service)
	const content = "Router setup. {{#if_subscribed}}Use the dashboard.{{/if_subscribed}}{{#if_not_subscribed}}Choose a plan.{{/if_not_subscribed}}"
	if err := create.CreateDocument(&types.CreateDocumentRequest{Title: "Router guide", Content: content, Tags: []string{"setup", "router"}, Show: &published}); err != nil {
		t.Fatal(err)
	}
	if err := create.CreateDocument(&types.CreateDocumentRequest{Title: "Router FAQ", Content: "Draft FAQ", Tags: []string{"router-legacy"}, Show: &draft}); err != nil {
		t.Fatal(err)
	}
	search := NewGetDocumentListLogic(ctx, service)
	result, err := search.GetDocumentList(&types.GetDocumentListRequest{Page: 1, Size: 1, Tag: "router", Search: "dashboard"})
	if err != nil || result.Total != 1 || len(result.List) != 1 || result.List[0].Title != "Router guide" || !slices.Equal(result.List[0].Tags, []string{"setup", "router"}) {
		t.Fatalf("full tag and content search = %+v, %v", result, err)
	}
	onlyExact, err := search.GetDocumentList(&types.GetDocumentListRequest{Page: 1, Size: 10, Tag: "router"})
	if err != nil || onlyExact.Total != 1 {
		t.Fatalf("exact tag search = %+v, %v", onlyExact, err)
	}
	id := result.List[0].Id
	detail, err := NewGetDocumentDetailLogic(ctx, service).GetDocumentDetail(&types.GetDocumentDetailRequest{Id: id})
	if err != nil || detail.Content != content {
		t.Fatalf("admin content = %+v, %v", detail, err)
	}

	member := client.User.Create().SetPassword("fixture").SaveX(ctx)
	expiredMember := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(member.ID).SetSubscribeID(plan.ID).SetOrderID(1).SetStatus(1).SetExpireTime(time.Now().Add(time.Hour)).SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(expiredMember.ID).SetSubscribeID(plan.ID).SetOrderID(2).SetStatus(3).SetExpireTime(time.Now().Add(-time.Hour)).SaveX(ctx)
	for _, tc := range []struct {
		uid  int64
		want string
	}{{member.ID, "Router setup. Use the dashboard."}, {expiredMember.ID, "Router setup. Choose a plan."}} {
		reader := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: tc.uid})
		got, err := publicdoc.NewQueryDocumentDetailLogic(reader, service).QueryDocumentDetail(&types.QueryDocumentDetailRequest{Id: id})
		if err != nil || got.Content != tc.want {
			t.Fatalf("reader %d document = %+v, %v", tc.uid, got, err)
		}
	}

	publicList := publicdoc.NewQueryDocumentListLogic(ctx, service)
	before, err := publicList.QueryDocumentList()
	if err != nil || before.Total != 1 || len(before.List) != 1 || before.List[0].Id != id || before.List[0].Content != "" {
		t.Fatalf("published catalog = %+v, %v", before, err)
	}
	if err := NewUpdateDocumentLogic(ctx, service).UpdateDocument(&types.UpdateDocumentRequest{Id: id, Title: "Archived guide", Content: content, Tags: []string{"archive"}, Show: &draft}); err != nil {
		t.Fatal(err)
	}
	after, err := publicList.QueryDocumentList()
	if err != nil || after.Total != 0 || len(after.List) != 0 {
		t.Fatalf("published catalog after unpublishing = %+v, %v", after, err)
	}
	if err := NewBatchDeleteDocumentLogic(ctx, service).BatchDeleteDocument(&types.BatchDeleteDocumentRequest{Ids: []string{strconv.FormatInt(id, 10)}}); err != nil {
		t.Fatal(err)
	}
	if remaining := client.Document.Query().OnlyX(ctx); remaining.Title != "Router FAQ" {
		t.Fatalf("expected only the draft to remain: %+v", remaining)
	}
}
