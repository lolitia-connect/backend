package emailLogic

import (
	"testing"
)

func TestResolveSubjectFallsBackToQueuedLiteral(t *testing.T) {
	got := resolveSubject("", "Verification code", map[string]interface{}{"Code": "1234"})
	if got != "Verification code" {
		t.Fatalf("resolveSubject fallback = %q", got)
	}
}

func TestResolveSubjectRendersConfiguredTemplate(t *testing.T) {
	got := resolveSubject("【{{.SiteName}}】订阅已到期", "Subscription Expired", map[string]interface{}{
		"SiteName": "Example",
	})
	if got != "【Example】订阅已到期" {
		t.Fatalf("resolveSubject = %q", got)
	}
}

func TestResolveSubjectSendsRawTextOnTemplateError(t *testing.T) {
	// A malformed configured subject must be sent as-is rather than silently
	// reverting to the English fallback.
	got := resolveSubject("{{.SiteName", "Fallback", map[string]interface{}{"SiteName": "Example"})
	if got != "{{.SiteName" {
		t.Fatalf("resolveSubject with malformed template = %q", got)
	}
}

func TestRenderEmailTemplateRejectsMalformedBody(t *testing.T) {
	if _, err := renderEmailTemplate("body", "{{.Broken", nil); err == nil {
		t.Fatal("malformed body template must return an error")
	}
	if out, err := renderEmailTemplate("body", "Hello {{.Name}}", map[string]interface{}{"Name": "World"}); err != nil || out != "Hello World" {
		t.Fatalf("renderEmailTemplate = %q, err=%v", out, err)
	}
}
