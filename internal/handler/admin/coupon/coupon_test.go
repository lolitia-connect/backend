package coupon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func TestCouponHandlersManageAndFilterPersistedCoupons(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}

	const path = "/v1/admin/coupon"
	request := func(handler func(*hertzx.Context), method, target, body string, wantCode uint32) json.RawMessage {
		t.Helper()
		raw := app.NewContext(0)
		raw.Request.Header.SetMethod(method)
		raw.Request.SetRequestURI(target)
		if body != "" {
			raw.Request.Header.Set("Content-Type", "application/json")
			raw.Request.SetBodyString(body)
		}
		handler(hertzx.NewContext(t.Context(), raw))
		var response struct {
			Code uint32          `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw.Response.Body(), &response); err != nil {
			t.Fatalf("%s %s returned malformed JSON: %v", method, target, err)
		}
		if raw.Response.StatusCode() != http.StatusOK || response.Code != wantCode {
			t.Fatalf("%s %s returned HTTP %d, payload %s (want code %d)", method, target, raw.Response.StatusCode(), raw.Response.Body(), wantCode)
		}
		return response.Data
	}
	list := func(query string) types.GetCouponListResponse {
		t.Helper()
		var response types.GetCouponListResponse
		body := request(GetCouponListHandler(service), http.MethodGet, path+"/list?page=1&size=1"+query, "", http.StatusOK)
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("decode coupon list: %v", err)
		}
		return response
	}

	const create = `{"name":"Launch offer","code":"LAUNCH","type":1,"discount":250,"start_time":1,"expire_time":2147483647,"subscribe":["12","34"]}`
	request(CreateCouponHandler(service), http.MethodPost, path, create, http.StatusOK)
	request(CreateCouponHandler(service), http.MethodPost, path, create, xerr.DatabaseInsertError)
	request(CreateCouponHandler(service), http.MethodPost, path, `{"name":"Launch invalid"}`, xerr.InvalidParams)
	request(CreateCouponHandler(service), http.MethodPost, path, `{"name":"Holiday offer","type":1,"discount":500,"start_time":1,"expire_time":2147483647,"subscribe":["123"]}`, http.StatusOK)
	if got := list(""); got.Total != 2 || len(got.List) != 1 {
		t.Fatalf("pagination after duplicate and invalid create: %+v", got)
	}
	launch := list("&subscribe=12&search=LAU")
	if launch.Total != 1 || len(launch.List) != 1 || launch.List[0].Code != "LAUNCH" || launch.List[0].Discount != 250 || !launch.List[0].Enable || !slices.Equal(launch.List[0].Subscribe, []string{"12", "34"}) {
		t.Fatalf("filtered launch coupon: %+v", launch)
	}
	if got := list("&subscribe=12"); got.Total != 1 {
		t.Fatalf("plan filter matched a partial plan ID: %+v", got)
	}
	holiday := list("&subscribe=123")
	if holiday.Total != 1 || len(holiday.List) != 1 || holiday.List[0].Code == "" || holiday.List[0].Code == "LAUNCH" {
		t.Fatalf("coupon with generated code: %+v", holiday)
	}

	id := launch.List[0].Id
	update := fmt.Sprintf(`{"id":%q,"name":"Launch offer","code":"LAUNCH","type":1,"discount":300,"start_time":1,"expire_time":2147483647,"subscribe":["34"],"enable":false}`, fmt.Sprint(id))
	request(UpdateCouponHandler(service), http.MethodPut, path, update, http.StatusOK)
	if got := list("&subscribe=12"); got.Total != 0 {
		t.Fatalf("old plan still matched after update: %+v", got)
	}
	if got := list("&subscribe=34"); got.Total != 1 || len(got.List) != 1 || got.List[0].Id != id || got.List[0].Enable || got.List[0].Discount != 300 {
		t.Fatalf("updated coupon: %+v", got)
	}

	request(DeleteCouponHandler(service), http.MethodDelete, path, fmt.Sprintf(`{"id":%d}`, id), xerr.InvalidParams)
	if got := list("&search=LAU"); got.Total != 1 {
		t.Fatalf("invalid delete changed state: %+v", got)
	}
	request(DeleteCouponHandler(service), http.MethodDelete, path, fmt.Sprintf(`{"id":"%d"}`, id), http.StatusOK)
	request(BatchDeleteCouponHandler(service), http.MethodDelete, path+"/batch", fmt.Sprintf(`{"ids":["%d","%d"]}`, id, holiday.List[0].Id), http.StatusOK)
	if got := list(""); got.Total != 0 || len(got.List) != 0 {
		t.Fatalf("coupons remained after delete: %+v", got)
	}
}
