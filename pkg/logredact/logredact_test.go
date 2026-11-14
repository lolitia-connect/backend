package logredact

import (
	"strings"
	"testing"
)

func TestSensitiveKeyRedactsKnownNames(t *testing.T) {
	for _, key := range []string{
		"token", "access_token", "password", "client_secret", "authorization",
		"cookie", "signature", "session_id", "uuid", "request_body", "response",
		"payload", "sql", "user", "order_info", "subscribe", "template", "value",
		"email", "recovery_email", "phone", "ip", "user_agent", "device_id",
		"group_chat_id", "redirect_url",
	} {
		t.Run(key, func(t *testing.T) {
			if !SensitiveKey(key) {
				t.Fatalf("key %q is not classified as sensitive", key)
			}
		})
	}
}

func TestSensitiveKeyPreservesSafeOperationalKeys(t *testing.T) {
	for _, key := range []string{"status", "method", "route", "duration", "request_id", "user_id", "order_no"} {
		t.Run(key, func(t *testing.T) {
			if SensitiveKey(key) {
				t.Fatalf("safe key %q was classified as sensitive", key)
			}
		})
	}
}

func TestTextRemovesCommonCredentialsAndEmail(t *testing.T) {
	secrets := []string{
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.VeryLongSignatureValue",
		"123456789:AAabcdefghijklmnopqrstuvwxyz1234",
		"alice@example.com",
		"Bearer top-secret-token",
		"query-secret",
	}
	input := strings.Join(secrets[:4], " ") + " https://example.test/callback?token=" + secrets[4]
	got := Text(input)

	for _, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, RedactedValue) {
		t.Fatalf("redacted text does not contain replacement marker: %s", got)
	}
}

// The subscription URL carries the user's token as a query parameter, so the
// access log has to strip it while keeping the endpoint readable.
func TestTextRedactsTokenQueryParameter(t *testing.T) {
	got := Text("GET /v1/subscribe?token=abcdef123456&flag=clash")
	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("query token survived redaction: %s", got)
	}
	if !strings.Contains(got, "flag=clash") {
		t.Fatalf("harmless query parameter was dropped: %s", got)
	}
}

// The node API authenticates with ?secret_key=, and a key that only matches on
// an exact alternative would let the whole node credential through.
func TestTextRedactsCompoundCredentialQueryParameters(t *testing.T) {
	for _, query := range []string{
		"secret_key=nodecredential",
		"api_key=paymentcredential",
		"private_key=signingcredential",
		"client_secret=oauthcredential",
		"subscribe_token=subscriptioncredential",
		"access_token=jwtcredential",
	} {
		t.Run(query, func(t *testing.T) {
			got := Text("GET /v1/thing?" + query)
			name, credential, _ := strings.Cut(query, "=")
			if strings.Contains(got, credential) {
				t.Fatalf("%s value survived redaction: %s", name, got)
			}
		})
	}
}

func TestValueRecursesThroughStructuredPayloads(t *testing.T) {
	value := map[string]any{
		"safe": "ok",
		"nested": map[string]any{
			"token": "subscription-secret",
			"note":  "contact person@example.com",
		},
		"items": []any{map[string]string{"password": "secret", "status": "ready"}},
	}

	redacted, ok := Value(value).(map[string]any)
	if !ok {
		t.Fatalf("redacted value has type %T", Value(value))
	}
	nested := redacted["nested"].(map[string]any)
	if nested["token"] != RedactedValue || nested["note"] != "contact "+RedactedValue {
		t.Fatalf("nested payload was not redacted: %#v", nested)
	}
	items := redacted["items"].([]any)
	item := items[0].(map[string]string)
	if item["password"] != RedactedValue || item["status"] != "ready" {
		t.Fatalf("slice payload was not redacted: %#v", items)
	}
}

func TestValueLeavesUnmodelledTypesAlone(t *testing.T) {
	if got := Value(42); got != 42 {
		t.Fatalf("Value(42) = %#v, want 42", got)
	}
	if got := Value(true); got != true {
		t.Fatalf("Value(true) = %#v, want true", got)
	}
	if got := Value(nil); got != nil {
		t.Fatalf("Value(nil) = %#v, want nil", got)
	}
}

func TestValueRedactsErrors(t *testing.T) {
	got, ok := Value(errString("login failed for alice@example.com")).(string)
	if !ok {
		t.Fatalf("Value(error) has type %T", Value(errString("x")))
	}
	if strings.Contains(got, "alice@example.com") {
		t.Fatalf("error text was not redacted: %s", got)
	}
}

func TestTextLeavesEmptyAndPlainTextAlone(t *testing.T) {
	if got := Text(""); got != "" {
		t.Fatalf("Text(\"\") = %q, want empty", got)
	}
	const plain = "HTTP Request status=200 route=/v1/user"
	if got := Text(plain); got != plain {
		t.Fatalf("Text(%q) = %q, want it unchanged", plain, got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
