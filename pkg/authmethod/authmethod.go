package authmethod

import "strings"

const (
	Email  = "email"  //邮箱
	Mobile = "mobile" //手机
	Device = "device" //设备

)

// CanonicalEmail returns the canonical storage form of an email identifier.
// Emails are case-insensitive, so storing them lower-cased + trimmed prevents
// the same mailbox from being registered or looked up multiple times.
func CanonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CanonicalIdentifier normalizes an auth identifier for its auth type.
// Only email identifiers are canonicalized; other types are stored verbatim
// because their values (OAuth subject ids, phone numbers) are case-sensitive
// or carry meaningful formatting.
func CanonicalIdentifier(authType, identifier string) string {
	if authType == Email {
		return CanonicalEmail(identifier)
	}
	return identifier
}
