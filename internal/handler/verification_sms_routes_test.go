package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/limit"
	smsworker "github.com/perfect-panel/server/queue/logic/sms"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestSMSLoginDeliveryReceipt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		gatewayCode int
		wantStatus  int
	}{
		{"delivered", 200, 1},
		{"gateway rejects the message", 403, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type message struct {
				Area    string `json:"mobileArea"`
				Number  string `json:"mobiles"`
				Content string `json:"content"`
			}
			messages := make(chan message, 1)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var msg message
				if r.Method != http.MethodPost || r.URL.Path != "/v2/api/sendSMS" || json.NewDecoder(r.Body).Decode(&msg) != nil {
					http.Error(w, "unexpected gateway request", http.StatusBadRequest)
					return
				}
				messages <- msg
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"code":%d}`, tc.gatewayCode)
			}))
			t.Cleanup(gateway.Close)

			f := newRouteFixture(t)
			ctx := t.Context()
			const number = "2025550123"
			account, _ := f.account(false)
			f.client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("mobile").SetAuthIdentifier("+1" + number).SetVerified(true).SaveX(ctx)
			inspector := f.startQueue()
			_, operator := f.account(true)
			method := f.client.AuthMethod.Create().SetMethod("mobile").SetConfig(`{}`).SetEnabled(false).SaveX(ctx)
			const settings = "/v1/admin/auth-method"
			payload := fmt.Sprintf(`{"id":"%d","method":"mobile","enabled":true,"config":{"platform":"abosend","platform_config":{"api_domain":%q,"access":"fixture","secret":"fixture","template":"Login code {{.code}}"}}}`, method.ID, gateway.URL)
			f.request(http.MethodPut, settings+"/config", operator, payload, http.StatusOK)
			var saved types.GetAuthMethodListResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, settings+"/list", operator, "", http.StatusOK), &saved))
			require.Len(t, saved.List, 1)
			require.Equal(t, method.ID, saved.List[0].Id)
			require.True(t, saved.List[0].Enabled)
			probeStatus := uint32(http.StatusOK)
			if tc.gatewayCode != 200 {
				probeStatus = http.StatusInternalServerError
			}
			f.request(http.MethodPost, settings+"/test_sms_send", operator, `{"area_code":"1","telephone":"2025550199"}`, probeStatus)
			select {
			case probe := <-messages:
				require.Equal(t, message{Area: "+1", Number: "12025550199", Content: "Login code 123456"}, probe)
			default:
				t.Fatal("the configured provider did not receive the operator's test message")
			}
			f.service.Config.JwtAuth.AccessExpire = 300
			f.service.Config.VerifyCode.ExpireTime = 300
			f.service.AuthLimiter = limit.NewPeriodLimit(86400, 10, f.service.Redis, "fixture:sms:")
			request := fmt.Sprintf(`{"telephone_area_code":"1","telephone":%q,"type":%d}`, number, constant.Security)
			var sent types.SendCodeResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/common/send_sms_code", "", request, http.StatusOK), &sent))
			require.True(t, sent.Status)
			require.Empty(t, sent.Code)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			require.Equal(t, queue.ForthwithSendSms, jobs[0].Type)
			require.NoError(t, smsworker.NewSendSmsLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
			var delivered message
			select {
			case delivered = <-messages:
			default:
				t.Fatal("worker did not submit a message to the configured gateway")
			}
			require.Equal(t, "+1", delivered.Area)
			require.Equal(t, "1"+number, delivered.Number)
			entry := f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeMobileMessage.Uint8())).OnlyX(ctx)
			var receipt log.Message
			require.NoError(t, json.Unmarshal([]byte(entry.Content), &receipt))
			require.Equal(t, "+1"+number, receipt.To)
			require.EqualValues(t, tc.wantStatus, receipt.Status)
			if tc.gatewayCode != 200 {
				return
			}
			code, ok := strings.CutPrefix(delivered.Content, "Login code ")
			require.True(t, ok)
			require.Len(t, code, 6)
			login := fmt.Sprintf(`{"telephone_area_code":"1","telephone":%q,"telephone_code":%q}`, number, code)
			var session types.LoginResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/login/telephone", "", login, http.StatusOK), &session))
			authenticated, err := middleware.AuthenticateRequest(ctx, f.service, session.Token, "/v1/public/user/info")
			require.NoError(t, err)
			require.Equal(t, account.ID, authenticated.Value(constant.CtxKeyUser).(*user.User).Id)
		})
	}
}
