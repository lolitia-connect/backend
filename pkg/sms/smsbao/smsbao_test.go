package smsbao

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendCodeUsesRegionalGatewayAndReturnsProviderResult(t *testing.T) {
	const template = "登录验证码 {{.code}}"
	client := NewClient(Config{Access: "fixture", Secret: "test-password", Template: template})
	for _, tc := range []struct{ area, mobile, path, recipient, reply, wantError string }{
		{"86", "13800000000", "/sms", "13800000000", "0", ""},
		{"44", "7700900123", "/wsms", "+447700900123", "41", "Insufficient balance"},
		{"44", "7700900123", "/wsms", "+447700900123", "unavailable", "unknown error"},
	} {
		t.Run(tc.area+"/"+tc.reply, func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				want := url.Values{"u": {"fixture"}, "p": {fmt.Sprintf("%x", md5.Sum([]byte("test-password")))}, "m": {tc.recipient}, "c": {"登录验证码 085293"}}
				if r.URL.Query().Encode() != want.Encode() {
					t.Errorf("unexpected message parameters %v", r.URL.Query())
				}
				fmt.Fprint(w, tc.reply)
			}))
			t.Cleanup(gateway.Close)
			client.client.SetBaseURL(gateway.URL)
			err := client.SendCode(tc.area, tc.mobile, "085293")
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.wantError)
			}
		})
	}
}
