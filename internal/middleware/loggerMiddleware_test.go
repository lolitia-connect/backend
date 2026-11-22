package middleware

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/pkg/logredact"
)

func TestOnlyKnownRefreshRejectionDowngrades503(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Response.SetStatusCode(consts.StatusServiceUnavailable)
	ctx.Response.Header.Set("Retry-After", "1")
	if isExpectedRetry(ctx) {
		t.Fatal("a genuine 503 was classified as an expected retry")
	}
	ctx.Error(node.ErrServerBusy)
	if !isExpectedRetry(ctx) {
		t.Fatal("the node refresh refusal was classified as a service failure")
	}
}

// A subscription URL carries the user's token as a query parameter and the
// access log writes the whole request line, so the token must not survive into
// the sink.
func TestLoggedRequestLineRedactsSubscriptionToken(t *testing.T) {
	requestLine := "GET example.test/v1/subscribe?token=abcdef123456&flag=clash"
	got := logredact.Text(requestLine)

	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("subscription token survived redaction: %s", got)
	}
	if !strings.Contains(got, "flag=clash") {
		t.Fatalf("harmless query parameter was dropped: %s", got)
	}
}

func TestLoggedQueryRedactsNodeSecret(t *testing.T) {
	got := logredact.Text("secret_key=nodecredential&protocol=vless")

	if strings.Contains(got, "nodecredential") {
		t.Fatalf("node secret survived redaction: %s", got)
	}
}

// Bodies are masked by key first and then redacted, so a credential that sits
// in a field the key list does not know about is still removed.
func TestLoggedBodyRedactsBeyondKeyMasking(t *testing.T) {
	body := []byte(`{"email":"alice@example.com","password":"hunter2","api_token":"nkp_private_key","note":"ping admin@example.com"}`)

	masked := maskSensitiveFields(body, []string{"password", "old_password", "new_password", "api_token"})
	if strings.Contains(string(masked), "hunter2") || strings.Contains(string(masked), "nkp_private_key") {
		t.Fatalf("key masking left the password in place: %s", masked)
	}

	got := logredact.Text(string(masked))
	if strings.Contains(got, "alice@example.com") || strings.Contains(got, "admin@example.com") {
		t.Fatalf("redaction left an e-mail address in place: %s", got)
	}
	if !strings.Contains(got, "ping") {
		t.Fatalf("redaction dropped the surrounding diagnostic text: %s", got)
	}
}

// maskSensitiveFields must not corrupt a body it cannot parse.
func TestMaskSensitiveFieldsLeavesNonJSONAlone(t *testing.T) {
	body := []byte("not json at all")
	if got := maskSensitiveFields(body, []string{"password"}); string(got) != string(body) {
		t.Fatalf("maskSensitiveFields = %q, want %q", got, body)
	}
}
