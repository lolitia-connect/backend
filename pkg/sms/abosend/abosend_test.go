package abosend_test

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perfect-panel/server/pkg/sms"
	"github.com/stretchr/testify/require"
)

func TestConfiguredSenderDeliversSignedVerificationAndRequiresProviderAcceptance(t *testing.T) {
	const secret = "fixture-sms-signing-key"
	const message = "Verification code: 001729"
	for _, tc := range []struct {
		name, receipt, wantError string
		status                   int
	}{
		{"accepted", `{"code":200,"data":{"sendCode":"accepted-1"}}`, "", 200},
		{"rejected", `{"code":402,"message":"insufficient credit"}`, "insufficient credit", 200},
		{"invalid receipt", `{"code":`, "unmarshal", 200},
		{"service offline", ``, "503", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type delivery struct{ OrgCode, MobileArea, Mobiles, Content, Rand, Sign string }
			requests := make(chan delivery, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v2/api/sendSMS" {
					http.NotFound(w, r)
					return
				}
				var body delivery
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid body", http.StatusBadRequest)
					return
				}
				requests <- body
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.receipt)
			}))
			t.Cleanup(server.Close)
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(previous) })
			cfg := fmt.Sprintf(`{"access":"fixture-org","secret":%q,"template":"Verification code: {{.code}}","api_domain":%q}`, secret, server.URL)
			sender, err := sms.NewSender("abosend", cfg)
			require.NoError(t, err)
			require.Equal(t, message, sender.GetSendCodeContent("001729"))
			err = sender.SendCode("44", "7700900123", "001729")
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantError)
			}
			select {
			case sent := <-requests:
				require.Equal(t, "fixture-org", sent.OrgCode)
				require.Equal(t, "+44", sent.MobileArea)
				require.Equal(t, "447700900123", sent.Mobiles)
				require.Equal(t, message, sent.Content)
				require.NotEmpty(t, sent.Rand)
				require.Equal(t, fmt.Sprintf("%X", md5.Sum([]byte("fixture-org"+message+sent.Rand+secret))), sent.Sign)
			default:
				t.Fatal("the configured provider did not receive a verification message")
			}
			require.NotContains(t, output.String(), secret)
		})
	}
}
