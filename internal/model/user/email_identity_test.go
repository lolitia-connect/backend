package user

import (
	"errors"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/pkg/authmethod"
)

func TestCanonicalAuthIdentifier(t *testing.T) {
	got, err := canonicalAuthIdentifier(authmethod.Email, "  User@Example.COM ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "user@example.com" {
		t.Fatalf("canonicalAuthIdentifier(email) = %q, want user@example.com", got)
	}

	if _, err := canonicalAuthIdentifier(authmethod.Email, "   "); !errors.Is(err, ErrInvalidEmailIdentity) {
		t.Fatalf("blank email should return ErrInvalidEmailIdentity, got %v", err)
	}

	// Non-email identifiers are never rejected even when empty (device rows etc.).
	got, err = canonicalAuthIdentifier("github", "")
	if err != nil {
		t.Fatalf("non-email identifier should not error: %v", err)
	}
	if got != "" {
		t.Fatalf("non-email identifier should be verbatim, got %q", got)
	}
}

func TestHasConflictingEmailIdentity(t *testing.T) {
	// Only the row with the same id is allowed; any other id is a conflict.
	if !hasConflictingEmailIdentity(1, []*ent.UserAuthMethod{{ID: 2}}) {
		t.Fatal("different id must be a conflict")
	}
	if !hasConflictingEmailIdentity(0, []*ent.UserAuthMethod{{ID: 5}, {ID: 6}}) {
		t.Fatal("pre-insert (id=0) with existing rows must conflict")
	}
	if hasConflictingEmailIdentity(7, []*ent.UserAuthMethod{{ID: 7}}) {
		t.Fatal("same id must not conflict")
	}
	if hasConflictingEmailIdentity(7, nil) {
		t.Fatal("no rows must not conflict")
	}
}

func TestResolveUniqueAuthMethod(t *testing.T) {
	if _, err := resolveUniqueAuthMethod(nil); !ent.IsNotFound(err) {
		t.Fatalf("zero rows should be NotFound, got %v", err)
	}

	one, err := resolveUniqueAuthMethod([]*ent.UserAuthMethod{{ID: 3, AuthIdentifier: "a@b.c"}})
	if err != nil {
		t.Fatalf("single row should resolve: %v", err)
	}
	if one == nil || one.Id != 3 {
		t.Fatalf("single row resolved incorrectly: %+v", one)
	}

	if _, err := resolveUniqueAuthMethod([]*ent.UserAuthMethod{{ID: 1}, {ID: 2}}); !errors.Is(err, ErrAmbiguousEmailIdentity) {
		t.Fatalf("two rows should be ambiguous, got %v", err)
	}
}
