package application

import (
	"database/sql"
	"encoding/base64"
	"testing"

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

func TestApplicationPreviewAndCatalogUseUpdatedConfiguration(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	const template = `mode={{index .Params "mode"}}`
	created, err := NewCreateSubscribeApplicationLogic(ctx, service).CreateSubscribeApplication(&types.CreateSubscribeApplicationRequest{
		Name: "Desktop", UserAgent: "DesktopClient", IsDefault: true, SubscribeTemplate: template,
		DefaultParams: "mode=rule", DownloadLink: types.DownloadLink{Windows: "https://example.test/desktop"},
	})
	if err != nil || created.Id == 0 {
		t.Fatalf("created client application = %+v, %v", created, err)
	}
	preview := NewPreviewSubscribeTemplateLogic(ctx, service)
	req := &types.PreviewSubscribeTemplateRequest{Id: created.Id}
	first, err := preview.PreviewSubscribeTemplate(req)
	if err != nil || first.Template != "mode=rule" {
		t.Fatalf("initial preview = %+v, %v", first, err)
	}
	download := "https://example.test/desktop-v2"
	_, err = NewUpdateSubscribeApplicationLogic(ctx, service).UpdateSubscribeApplication(&types.UpdateSubscribeApplicationRequest{
		Id: created.Id, Name: "Desktop v2", UserAgent: "DesktopClient", SubscribeTemplate: template,
		DefaultParams: "mode=global", OutputFormat: "base64", DownloadLink: types.DownloadLink{Windows: download},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := preview.PreviewSubscribeTemplate(req)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(second.Template)
	if err != nil || string(decoded) != "mode=global" {
		t.Fatalf("updated preview = %q, %v", decoded, err)
	}
	admin, err := NewGetSubscribeApplicationListLogic(ctx, service).GetSubscribeApplicationList(&types.GetSubscribeApplicationListRequest{})
	if err != nil || admin.Total != 1 || len(admin.List) != 1 || admin.List[0].Id != created.Id || admin.List[0].DefaultParams != "mode=global" || admin.List[0].OutputFormat != "base64" {
		t.Fatalf("admin application catalog = %+v, %v", admin, err)
	}
	catalog := common.NewGetClientLogic(ctx, service)
	public, err := catalog.GetClient()
	if err != nil || public.Total != 1 || len(public.List) != 1 || public.List[0].Name != "Desktop v2" || public.List[0].DownloadLink.Windows != download || public.List[0].IsDefault {
		t.Fatalf("public application catalog = %+v, %v", public, err)
	}
	if err := NewDeleteSubscribeApplicationLogic(ctx, service).DeleteSubscribeApplication(&types.DeleteSubscribeApplicationRequest{Id: created.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := preview.PreviewSubscribeTemplate(req); err == nil {
		t.Error("deleted application still has a preview")
	}
	public, err = catalog.GetClient()
	if err != nil || public.Total != 0 || len(public.List) != 0 {
		t.Fatalf("public application catalog after deletion = %+v, %v", public, err)
	}
}
