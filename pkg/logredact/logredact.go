// Package logredact strips credentials, message bodies and personal
// identifiers out of values on their way to a log sink.
//
// The policy is deliberately broad: losing a diagnostic value is preferable to
// persisting a credential or a personal identifier in the log database. Ported
// from upstream v1.20.3 (pkg/logger/redact.go), adapted to the fork's zap-only
// logging surface.
package logredact

import (
	"fmt"
	"regexp"
	"strings"
)

// RedactedValue replaces everything the policy considers sensitive.
const RedactedValue = "[REDACTED]"

// maxDepth bounds recursion into nested payloads so a self-referential value
// cannot spin the logger.
const maxDepth = 8

var (
	jwtPattern              = regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	telegramBotTokenPattern = regexp.MustCompile(`\b[0-9]{6,12}:AA[A-Za-z0-9_-]{20,}\b`)
	emailPattern            = regexp.MustCompile(`\b[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+\b`)
	bearerPattern           = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	// The prefix is captured so the parameter name survives: the query string a
	// logger hands over usually has no leading "?", so anchoring on [?&] alone
	// would leave the first parameter unredacted.
	sensitiveQueryPattern = regexp.MustCompile(`(?i)(^|[?&])((?:access_?token|subscribe_?token|auth_?code|api_?key|secret_?key|private_?key|client_?secret|code|key|password|secret|signature|token)=)[^&#\s]+`)
)

var sensitiveMarkers = []string{
	"token", "secret", "password", "passwd", "credential", "authorization",
	"cookie", "signature", "privatekey", "apikey", "accesskey", "session", "uuid",
	"email", "telephone", "phone", "useragent", "clientip", "deviceid",
	"chatid", "openid", "uniqueid", "senderid",
}

// sensitiveKeyNames are whole keys whose value never belongs in a log, even
// though the name carries no marker from sensitiveMarkers.
var sensitiveKeyNames = map[string]struct{}{
	"body": {}, "requestbody": {}, "responsebody": {}, "request": {}, "response": {},
	"payload": {}, "content": {}, "query": {}, "params": {}, "form": {}, "config": {},
	"headers": {}, "header": {}, "sql": {}, "user": {}, "users": {}, "userinfo": {},
	"order": {}, "orders": {}, "orderinfo": {}, "subscribe": {}, "subscribers": {},
	"req": {}, "template": {}, "value": {}, "cachekey": {}, "coupon": {},
	"identifier": {}, "recipient": {}, "subject": {}, "text": {}, "command": {},
	"redirect": {}, "redirecturl": {}, "url": {}, "refercode": {}, "deductionlog": {},
	// A bare "ip" carries no marker and is a personal identifier on its own.
	"ip": {},
}

// SensitiveKey reports whether a field of this name must be redacted. A caller
// that owns the key but not the value (a custom zap encoder, say) can use it to
// decide before formatting anything.
func SensitiveKey(key string) bool {
	compact := normalizeKey(key)
	for _, marker := range sensitiveMarkers {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	_, ok := sensitiveKeyNames[compact]
	return ok
}

func normalizeKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	return strings.ReplaceAll(normalized, "_", "")
}

// Text removes the common credential shapes from a message: JWTs, Telegram bot
// tokens, e-mail addresses, bearer headers and sensitive query parameters.
func Text(value string) string {
	if value == "" {
		return value
	}
	value = jwtPattern.ReplaceAllString(value, RedactedValue)
	value = telegramBotTokenPattern.ReplaceAllString(value, RedactedValue)
	value = emailPattern.ReplaceAllString(value, RedactedValue)
	value = bearerPattern.ReplaceAllString(value, "Bearer "+RedactedValue)
	return sensitiveQueryPattern.ReplaceAllString(value, `${1}${2}`+RedactedValue)
}

// Value applies Text to the leaf strings of a structured value, redacting
// whole entries whose key the policy classifies as sensitive.
func Value(value any) any {
	return redact(value, 0)
}

func redact(value any, depth int) any {
	if depth > maxDepth {
		return RedactedValue
	}
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return Text(v)
	case error:
		return Text(v.Error())
	case fmt.Stringer:
		return Text(v.String())
	case []byte:
		return Text(string(v))
	case []string:
		redacted := make([]string, len(v))
		for i, item := range v {
			redacted[i] = Text(item)
		}
		return redacted
	case map[string]string:
		redacted := make(map[string]string, len(v))
		for key, item := range v {
			if SensitiveKey(key) {
				redacted[key] = RedactedValue
				continue
			}
			redacted[key] = Text(item)
		}
		return redacted
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, item := range v {
			if SensitiveKey(key) {
				redacted[key] = RedactedValue
				continue
			}
			redacted[key] = redact(item, depth+1)
		}
		return redacted
	case []any:
		redacted := make([]any, len(v))
		for i, item := range v {
			redacted[i] = redact(item, depth+1)
		}
		return redacted
	default:
		return value
	}
}
