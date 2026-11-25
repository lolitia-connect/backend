package twilio

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	sdk "github.com/twilio/twilio-go/client"
)

type messageTransport func(*http.Request) (*http.Response, error)

func (send messageTransport) RoundTrip(req *http.Request) (*http.Response, error) { return send(req) }

func TestVerificationSMSUsesConfiguredSenderAndReportsDeliveryFailure(t *testing.T) {
	const account, secret = "fixtureaccount", "fixturesecret"
	client := NewClient(Config{Access: account, Secret: secret, PhoneNumber: "+12025550101", Template: "Code: {{.code}}"})
	for _, tc := range []struct {
		name, reply, wantErr string
		status               int
	}{
		{"queued", `{"sid":"fixture-message","status":"queued"}`, "", http.StatusCreated},
		{"rejected", `{"code":21211,"message":"Invalid destination"}`, "Invalid destination", http.StatusBadRequest},
		{"delivery failure", `{"status":"failed","error_code":30003,"error_message":"Handset unavailable"}`, "Handset unavailable", http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client.client.Client.(*sdk.Client).HTTPClient = &http.Client{Transport: messageTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "https://api.twilio.com/2010-04-01/Accounts/"+account+"/Messages.json", req.URL.String())
				name, password, ok := req.BasicAuth()
				require.True(t, ok)
				require.Equal(t, account, name)
				require.Equal(t, secret, password)
				require.NoError(t, req.ParseForm())
				require.Equal(t, url.Values{"To": {"+447700900123"}, "From": {"+12025550101"}, "Body": {"Code: 482951"}}, req.PostForm)
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.reply))}, nil
			})}
			err := client.SendCode("44", "7700900123", "482951")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, requests)
		})
	}
}
