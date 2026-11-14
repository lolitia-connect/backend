package authmethod

import "testing"

func TestCanonicalEmail(t *testing.T) {
	cases := map[string]string{
		"  User@Example.COM ": "user@example.com",
		"USER@EXAMPLE.COM":    "user@example.com",
		"user@example.com":    "user@example.com",
		"":                    "",
		"   ":                 "",
		"\tA@B.c\n":           "a@b.c",
	}
	for input, want := range cases {
		if got := CanonicalEmail(input); got != want {
			t.Fatalf("CanonicalEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalIdentifier_EmailIsCanonicalized(t *testing.T) {
	if got := CanonicalIdentifier(Email, "  Foo@Bar.com "); got != "foo@bar.com" {
		t.Fatalf("CanonicalIdentifier(email) = %q, want foo@bar.com", got)
	}
}

func TestCanonicalIdentifier_NonEmailIsVerbatim(t *testing.T) {
	// OAuth subject ids and phone numbers are case-sensitive / format-sensitive
	// and must never be lower-cased or trimmed.
	cases := []struct {
		authType   string
		identifier string
	}{
		{Mobile, "+1 555 0100"},
		{Device, "ABC-123"},
		{"github", "User-XYZ"},
		{"facebook", "  spaced  "},
	}
	for _, c := range cases {
		if got := CanonicalIdentifier(c.authType, c.identifier); got != c.identifier {
			t.Fatalf("CanonicalIdentifier(%q, %q) = %q, want verbatim", c.authType, c.identifier, got)
		}
	}
}
