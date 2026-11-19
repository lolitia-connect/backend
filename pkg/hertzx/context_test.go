package hertzx

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestShouldBindReadRequestsUseQueryWithJSONContentType(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			raw := app.NewContext(0)
			raw.Request.Header.SetMethod(method)
			raw.Request.Header.Set("Content-Type", "application/json")
			raw.Request.SetRequestURI("/list?page=2&enable=false&status=0&id[]=7&id[]=9")
			var request struct {
				Page   int     `form:"page"`
				Enable *bool   `form:"enable"`
				Status *int    `form:"status"`
				IDs    []int64 `form:"id"`
			}
			if err := NewContext(context.Background(), raw).ShouldBind(&request); err != nil {
				t.Fatal(err)
			}
			if request.Page != 2 || request.Enable == nil || *request.Enable || request.Status == nil || *request.Status != 0 || len(request.IDs) != 2 || request.IDs[0] != 7 || request.IDs[1] != 9 {
				t.Fatalf("query filters not bound: %+v", request)
			}
		})
	}
}

func TestShouldBindJSONPreservesFalseAndZero(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodPut)
	raw.Request.Header.Set("Content-Type", "application/json")
	raw.Request.SetRequestURI("/update?enable=true&balance=100")
	raw.Request.SetBodyString(`{"user_id":"7","enable":false,"balance":0}`)
	var request struct {
		UserID  int64 `json:"user_id,string"`
		Enable  *bool `json:"enable"`
		Balance int64 `json:"balance"`
	}
	if err := NewContext(context.Background(), raw).ShouldBind(&request); err != nil {
		t.Fatal(err)
	}
	if request.UserID != 7 || request.Enable == nil || *request.Enable || request.Balance != 0 {
		t.Fatalf("JSON body lost explicit values: %+v", request)
	}
}
