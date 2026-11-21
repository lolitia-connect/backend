package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	adminNekoproxy "github.com/perfect-panel/server/internal/handler/admin/nekoproxy"
	adminSystem "github.com/perfect-panel/server/internal/handler/admin/system"
	adminUser "github.com/perfect-panel/server/internal/handler/admin/user"
	publicUser "github.com/perfect-panel/server/internal/handler/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
)

func TestHandlersRejectBindingErrorsBeforeBusinessLogic(t *testing.T) {
	service := &svc.ServiceContext{}
	for _, test := range []struct {
		name, method, target, body string
		handler                    func(*hertzx.Context)
	}{
		{"malformed Nekoproxy node ID", http.MethodPut, "/v1/admin/server/nekoproxy/binding", `{"node_id":1,"forward_id":"l4s-1","session_id":"session-1"}`, adminNekoproxy.BindHandler(service)},
		{"missing Nekoproxy session", http.MethodPut, "/v1/admin/server/nekoproxy/binding", `{"node_id":"1","forward_id":"l4s-1"}`, adminNekoproxy.BindHandler(service)},
		{"negative Nekoproxy sync ID", http.MethodPost, "/v1/admin/server/nekoproxy/sync", `{"node_id":"-1"}`, adminNekoproxy.SyncHandler(service)},
		{"invalid Nekoproxy token type", http.MethodPut, "/v1/admin/server/nekoproxy/config", `{"api_url":"https://example.test","api_token":42}`, adminNekoproxy.UpdateConfigHandler(service)},
		{"malformed user flag", http.MethodPut, "/v1/admin/user/basic", `{"user_id":"7","enable":"false"}`, adminUser.UpdateUserBasicInfoHandler(service)},
		{"malformed notification flag", http.MethodPut, "/v1/public/user/notify", `{"enable_login_notify":"false"}`, publicUser.UpdateUserNotifyHandler(service)},
		{"malformed configuration", http.MethodPut, "/v1/admin/system/invite", `{"forced_invite":true,"referral_percentage":"invalid"}`, adminSystem.UpdateInviteConfigHandler(service)},
		{"truncated configuration", http.MethodPut, "/v1/admin/system/invite", `{"forced_invite":true`, adminSystem.UpdateInviteConfigHandler(service)},
		{"invalid page", http.MethodGet, "/v1/admin/user/list?page=invalid&size=20", "", adminUser.GetUserListHandler(service)},
		{"overflowing user ID", http.MethodGet, "/v1/admin/user/list?page=1&size=20&user_id=9223372036854775808", "", adminUser.GetUserListHandler(service)},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("invalid request reached business logic: %v", recovered)
				}
			}()
			raw := app.NewContext(0)
			raw.Request.Header.SetMethod(test.method)
			raw.Request.SetRequestURI(test.target)
			if test.body != "" {
				raw.Request.Header.Set("Content-Type", "application/json")
				raw.Request.SetBodyString(test.body)
			}
			test.handler(hertzx.NewContext(context.Background(), raw))
			var response struct {
				Code uint32 `json:"code"`
			}
			if err := json.Unmarshal(raw.Response.Body(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != xerr.InvalidParams {
				t.Fatalf("response code = %d, want %d", response.Code, xerr.InvalidParams)
			}
		})
	}
}
