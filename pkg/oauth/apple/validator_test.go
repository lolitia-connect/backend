package apple

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidationClientUsesAppleFormProtocolForEveryTokenOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != ContentType || request.Header.Get("Accept") != AcceptHeader || request.Header.Get("User-Agent") != UserAgent {
			t.Fatalf("Apple headers = %v", request.Header)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.Form.Get("client_id") != "client" {
			t.Fatalf("Apple client id = %q", request.Form.Get("client_id"))
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/revoke" {
			_, _ = writer.Write([]byte(`{"revoked":true}`))
			return
		}
		_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","id_token":"id-token"}`))
	}))
	defer server.Close()
	client := &Client{
		config:        Config{ClientID: "client", RedirectURI: "https://panel.example/callback"},
		validationURL: server.URL + "/token",
		revokeURL:     server.URL + "/revoke",
		client:        server.Client(),
		secret:        "secret",
	}
	web, err := client.VerifyWebToken(context.Background(), "web-code")
	if err != nil || web.AccessToken != "access" || web.RefreshToken != "refresh" {
		t.Fatalf("web validation = %+v, %v", web, err)
	}
	var app ValidationResponse
	if err := client.VerifyAppToken(context.Background(), AppValidationTokenRequest{ClientID: "client", ClientSecret: "app-secret", Code: "app-code"}, &app); err != nil || app.AccessToken != "access" {
		t.Fatalf("app validation = %+v, %v", app, err)
	}
	var refreshed ValidationResponse
	if err := client.VerifyRefreshToken(context.Background(), ValidationRefreshRequest{ClientID: "client", ClientSecret: "refresh-secret", RefreshToken: "old-refresh"}, &refreshed); err != nil || refreshed.AccessToken != "access" {
		t.Fatalf("refresh validation = %+v, %v", refreshed, err)
	}
	var revoked map[string]bool
	if err := client.RevokeAccessToken(context.Background(), "access", &revoked); err != nil || !revoked["revoked"] {
		t.Fatalf("access revoke = %+v, %v", revoked, err)
	}
	if err := client.RevokeRefreshToken(context.Background(), "refresh", &revoked); err != nil || !revoked["revoked"] {
		t.Fatalf("refresh revoke = %+v, %v", revoked, err)
	}
}

func TestAppleTokenClaimsAndRequestErrors(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "apple-user", "email": "person@example.test"}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := GetUniqueID(token); err != nil || id != "apple-user" {
		t.Fatalf("Apple unique id = %q, %v", id, err)
	}
	claims, err := GetClaims(token)
	if err != nil || (*claims)["email"] != "person@example.test" {
		t.Fatalf("Apple claims = %v, %v", claims, err)
	}
	if _, err := GetUniqueID("malformed"); err == nil {
		t.Fatal("malformed Apple token was accepted")
	}
	client := NewWithOptions(ClientOptions{validationURL: "://invalid", client: http.DefaultClient})
	if _, err := client.VerifyWebToken(context.Background(), "code"); err == nil {
		t.Fatal("invalid Apple validation URL was accepted")
	}
	var response ValidationResponse
	if err := doRequest(context.Background(), http.DefaultClient, &response, "://invalid", url.Values{"code": {"fixture"}}); err == nil {
		t.Fatal("invalid Apple request URL was accepted")
	}
	if err := json.Unmarshal([]byte(`{"error":"invalid_grant"}`), &response); err != nil || response.Error != "invalid_grant" {
		t.Fatal("Apple error response fixture was invalid")
	}
}
