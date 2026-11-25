package subscribe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func TestCustomerNodeCatalogFiltersDisabledNodesAndRejectsExpiredSubscriptions(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	server := client.Server.Create().SetName("Primary").SetCountry("JP").SetCity("Tokyo").SaveX(ctx)
	addNode := func(name string, enabled bool) *ent.Node {
		t.Helper()
		return client.Node.Create().SetName(name).SetServerID(server.ID).SetEnabled(enabled).
			SetProtocol("vless").SetProtocolID("primary").SetAddress(name + ".example.test").SetPort(443).SaveX(ctx)
	}
	direct := addNode("direct", true)
	disabled := addNode("disabled", false)
	addNode("unrelated", true)
	plan := client.Subscribe.Create().SetName("Regional").SetNodes(fmt.Sprintf("%d,%d", direct.ID, disabled.ID)).SaveX(ctx)
	service.Config.Register.EnableTrial = true
	service.Config.Register.TrialSubscribe = plan.ID
	const credential = "b195dd73-f8fa-46ff-8209-cdc481a72f79"
	now := time.Now()
	sub := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetExpireTime(now.Add(time.Hour)).SetTraffic(1000).SetDownload(100).SetStatus(1).SetToken("fixture-token").SetUUID(credential).SaveX(ctx)
	logic := NewQueryUserSubscribeNodeListLogic(owner, service)
	result, err := logic.QueryUserSubscribeNodeList()
	if err != nil || len(result.List) != 1 || result.List[0].Id != sub.ID || !result.List[0].IsTryOut {
		t.Fatalf("customer subscriptions = %+v, %v", result, err)
	}
	nodes := result.List[0].Nodes
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want only the enabled node selected by the plan", len(nodes))
	}
	if got := nodes[0]; got.Id != direct.ID || got.Uuid != credential || got.Country != "JP" || got.Port != 443 || got.Address != "direct.example.test" {
		t.Fatalf("selected node = %+v", got)
	}

	client.UserSubscribe.UpdateOneID(sub.ID).SetStatus(2).SetExpireTime(now.Add(-24 * time.Hour)).SetFinishedAt(now).SaveX(ctx)
	var code *xerr.CodeError
	if result, err = logic.QueryUserSubscribeNodeList(); result != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.SubscribeExpired {
		t.Fatalf("expired subscription without a fallback group = %+v, %v", result, err)
	}
}
