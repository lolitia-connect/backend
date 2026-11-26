package alibabacloud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/stretchr/testify/require"
)

func TestSendCodeRequiresAnAcceptedTemplateReceipt(t *testing.T) {
	for _, tc := range []struct{ name, receipt, wantErr string }{
		{"accepted", `{"Code":"OK","BizId":"fixture-receipt"}`, ""},
		{"limit reached", `{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"Daily limit reached"}`, "Daily limit reached"},
		{"rejection without message", `{"Code":"isv.TEMPLATE_MISSING"}`, "isv.TEMPLATE_MISSING"},
		{"missing delivery status", `{"RequestId":"fixture-receipt"}`, "status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan url.Values, 1)
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "invalid form", http.StatusBadRequest)
					return
				}
				requests <- r.Form
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.receipt)
			}))
			t.Cleanup(relay.Close)
			client := NewClient(Config{Access: "fixture-access", Secret: "fixture-secret", Endpoint: strings.TrimPrefix(relay.URL, "http://"), SignName: "Member Portal", TemplateCode: "SMS_LOGIN"})
			client.client.Protocol = tea.String("HTTP")
			err := client.SendCode("44", "7700900123", "083951")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			select {
			case msg := <-requests:
				require.Equal(t, "447700900123", msg.Get("PhoneNumbers"))
				require.Equal(t, "Member Portal", msg.Get("SignName"))
				require.Equal(t, "SMS_LOGIN", msg.Get("TemplateCode"))
				require.JSONEq(t, `{"code":"083951"}`, msg.Get("TemplateParam"))
			default:
				t.Fatal("relay did not receive the verification message")
			}
		})
	}
}
