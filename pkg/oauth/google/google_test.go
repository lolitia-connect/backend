package google

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type profileTransport func(*http.Request) (*http.Response, error)

func (f profileTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGoogleProfileLookupRecoversAfterProviderFailure(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	client := New(&Config{ClientID: "panel-web"})
	const profile = `{"id":"member-42","email":"member@example.test","name":"Ada","verified_email":"true"}`
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"offline", 0, ""}, {"expired access token", http.StatusUnauthorized, profile},
		{"incomplete response", http.StatusOK, `{"id":"member-42"`}, {"recovered", http.StatusOK, profile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			http.DefaultTransport = profileTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != "https://www.googleapis.com/oauth2/v2/userinfo" || req.Header.Get("Authorization") != "Bearer profile-proof" {
					return nil, fmt.Errorf("unexpected profile request %s %s", req.Method, req.URL.Path)
				}
				if tc.status == 0 {
					return nil, errors.New("provider unavailable")
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			member, err := client.GetUserInfo("profile-proof")
			if tc.name != "recovered" {
				require.Error(t, err)
				require.Nil(t, member)
				return
			}
			require.NoError(t, err)
			require.Equal(t, &UserInfo{OpenID: "member-42", Email: "member@example.test", Name: "Ada", VerifiedEmail: true}, member)
		})
	}
}

func TestParseVerifiedEmail(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string false", "false", false},
		{"string garbage", "yes", false},
		{"nil", nil, false},
		{"number", float64(1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseVerifiedEmail(c.input); got != c.want {
				t.Fatalf("parseVerifiedEmail(%v) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

func TestNewOmitsPhoneScope(t *testing.T) {
	client := New(&Config{ClientID: "id", ClientSecret: "secret"})
	want := []string{"openid", "profile", "email"}
	if len(client.Scopes) != len(want) {
		t.Fatalf("Scopes = %v, want %v", client.Scopes, want)
	}
	for i, s := range want {
		if client.Scopes[i] != s {
			t.Fatalf("Scopes[%d] = %q, want %q", i, client.Scopes[i], s)
		}
	}
}
