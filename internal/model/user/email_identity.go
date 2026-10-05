package user

import (
	"context"
	"errors"

	"github.com/perfect-panel/server/ent"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	"github.com/perfect-panel/server/pkg/authmethod"
)

// Errors returned when email identities are ambiguous or invalid.
var (
	ErrAmbiguousEmailIdentity = errors.New("ambiguous email identity")
	ErrInvalidEmailIdentity   = errors.New("invalid email identity")
)

// canonicalAuthIdentifier normalizes an identifier for storage and lookup.
// Email identifiers become the canonical (trimmed, lower-cased) form, and an
// empty canonical email is rejected so that blank rows cannot be created.
func canonicalAuthIdentifier(authType, identifier string) (string, error) {
	canonical := authmethod.CanonicalIdentifier(authType, identifier)
	if authType == authmethod.Email && canonical == "" {
		return "", ErrInvalidEmailIdentity
	}
	return canonical, nil
}

// resolveUserAuthMethodByIdentifier looks up an auth method by its exact
// canonical identifier. For emails it additionally falls back to a
// case-insensitive match to keep legacy rows (stored before canonicalization)
// working, rejecting the lookup when more than one row folds to the same value.
func resolveUserAuthMethodByIdentifier(ctx context.Context, db *ent.Client, authType, identifier string) (*AuthMethods, error) {
	canonical, err := canonicalAuthIdentifier(authType, identifier)
	if err != nil {
		return nil, err
	}

	item, err := db.UserAuthMethod.Query().
		Where(entauth.AuthType(authType), entauth.AuthIdentifier(canonical)).
		First(ctx)
	if authType != authmethod.Email || err == nil {
		return entToAuthMethod(item), err
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	// Legacy fallback: match case-insensitively and require a unique result.
	items, err := db.UserAuthMethod.Query().
		Where(entauth.AuthType(authmethod.Email), entauth.AuthIdentifierEqualFold(canonical)).
		Limit(2).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return resolveUniqueAuthMethod(items)
}

// resolveUniqueAuthMethod enforces that at most one row matched.
func resolveUniqueAuthMethod(items []*ent.UserAuthMethod) (*AuthMethods, error) {
	switch len(items) {
	case 0:
		return nil, &ent.NotFoundError{}
	case 1:
		return entToAuthMethod(items[0]), nil
	default:
		return nil, ErrAmbiguousEmailIdentity
	}
}

// hasConflictingEmailIdentity reports whether any matched row belongs to a
// different auth method than the one being written.
func hasConflictingEmailIdentity(currentID int64, items []*ent.UserAuthMethod) bool {
	for _, item := range items {
		if item.ID != currentID {
			return true
		}
	}
	return false
}

// guardEmailIdentityWrite rejects writes that would let two different auth
// method rows share the same canonical email identity.
func guardEmailIdentityWrite(ctx context.Context, db *ent.Client, data *AuthMethods) error {
	if data.AuthType != authmethod.Email {
		return nil
	}
	q := db.UserAuthMethod.Query().
		Where(entauth.AuthType(authmethod.Email), entauth.AuthIdentifierEqualFold(data.AuthIdentifier)).
		Limit(2)
	if data.Id != 0 {
		q = q.Where(entauth.IDNEQ(data.Id))
	}
	items, err := q.All(ctx)
	if err != nil {
		return err
	}
	if hasConflictingEmailIdentity(data.Id, items) {
		return ErrAmbiguousEmailIdentity
	}
	return nil
}

// ValidateEmailIdentityUniqueness fails when any canonical email is shared by
// more than one auth method row. It is used as a startup / migration guard.
func (m *defaultUserModel) ValidateEmailIdentityUniqueness(ctx context.Context) error {
	rows, err := m.db.UserAuthMethod.Query().
		Where(entauth.AuthType(authmethod.Email)).
		Select(entauth.FieldAuthIdentifier).
		All(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]int64, len(rows))
	for _, row := range rows {
		key := authmethod.CanonicalEmail(row.AuthIdentifier)
		seen[key]++
		if seen[key] > 1 {
			return ErrAmbiguousEmailIdentity
		}
	}
	return nil
}
