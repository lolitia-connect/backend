package entitlement

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SourceApple = "apple"

	ModeAutoRenewable = "auto_renewable"

	BillingIntervalMonth = "month"
	BillingIntervalYear  = "year"

	StatusActive       = "active"
	StatusGrace        = "grace"
	StatusBillingRetry = "billing_retry"
	StatusExpired      = "expired"
	StatusRevoked      = "revoked"
)

var ErrInvalidCommand = errors.New("invalid entitlement command")

type ReconcileEntitlementCommand struct {
	Source          string
	Scope           string
	SubscriptionKey string
	TransactionKey  string
	UserID          int64
	PlanID          int64
	OrderID         int64
	Revision        int64
	Mode            string
	BillingInterval string
	Status          string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	GraceUntil      *time.Time
	RevokedAt       *time.Time
	AutoRenew       bool
	ResetTraffic    bool
}

func (c ReconcileEntitlementCommand) Normalize(now time.Time) (ReconcileEntitlementCommand, error) {
	c.Source = strings.TrimSpace(c.Source)
	c.Scope = strings.TrimSpace(c.Scope)
	c.SubscriptionKey = strings.TrimSpace(c.SubscriptionKey)
	c.TransactionKey = strings.TrimSpace(c.TransactionKey)
	c.Mode = strings.TrimSpace(c.Mode)
	c.BillingInterval = strings.TrimSpace(c.BillingInterval)
	c.Status = strings.TrimSpace(c.Status)

	if c.Source != SourceApple {
		return ReconcileEntitlementCommand{}, invalid("source must be %q", SourceApple)
	}
	if c.Mode != ModeAutoRenewable {
		return ReconcileEntitlementCommand{}, invalid("mode must be %q", ModeAutoRenewable)
	}
	if c.BillingInterval != BillingIntervalMonth && c.BillingInterval != BillingIntervalYear {
		return ReconcileEntitlementCommand{}, invalid("billing interval must be month or year")
	}
	switch c.Status {
	case StatusActive, StatusGrace, StatusBillingRetry, StatusExpired, StatusRevoked:
	default:
		return ReconcileEntitlementCommand{}, invalid("unsupported status %q", c.Status)
	}
	if c.Scope == "" || len(c.Scope) > 255 {
		return ReconcileEntitlementCommand{}, invalid("scope must be between 1 and 255 bytes")
	}
	if c.SubscriptionKey == "" || len(c.SubscriptionKey) > 255 {
		return ReconcileEntitlementCommand{}, invalid("subscription key must be between 1 and 255 bytes")
	}
	if c.TransactionKey == "" || len(c.TransactionKey) > 255 {
		return ReconcileEntitlementCommand{}, invalid("transaction key must be between 1 and 255 bytes")
	}
	if c.UserID <= 0 || c.PlanID <= 0 || c.OrderID <= 0 || c.Revision <= 0 {
		return ReconcileEntitlementCommand{}, invalid("user, plan, order, and revision IDs must be positive")
	}
	if c.PeriodStart.IsZero() || c.PeriodEnd.IsZero() || !c.PeriodEnd.After(c.PeriodStart) {
		return ReconcileEntitlementCommand{}, invalid("period must have a non-empty start before end")
	}
	if !now.IsZero() && c.PeriodStart.After(now) {
		return ReconcileEntitlementCommand{}, invalid("period start cannot be in the future")
	}
	if c.RevokedAt != nil && !now.IsZero() && c.RevokedAt.After(now) {
		return ReconcileEntitlementCommand{}, invalid("revocation cannot be in the future")
	}

	switch c.Status {
	case StatusGrace:
		if c.GraceUntil == nil || !c.GraceUntil.After(c.PeriodEnd) {
			return ReconcileEntitlementCommand{}, invalid("grace status requires a deadline after period end")
		}
	case StatusBillingRetry, StatusExpired, StatusRevoked:
		if c.Status != StatusRevoked && c.PeriodEnd.After(now) {
			return ReconcileEntitlementCommand{}, invalid("inactive status cannot have a future paid deadline")
		}
	}
	if c.Status != StatusGrace && c.GraceUntil != nil {
		return ReconcileEntitlementCommand{}, invalid("grace deadline requires grace status")
	}
	if c.Status == StatusRevoked && c.RevokedAt == nil {
		return ReconcileEntitlementCommand{}, invalid("revoked status requires revoked_at")
	}
	if c.Status != StatusRevoked && c.RevokedAt != nil {
		return ReconcileEntitlementCommand{}, invalid("revocation timestamp requires revoked status")
	}

	return c, nil
}

func (c ReconcileEntitlementCommand) Validate(now time.Time) error {
	_, err := c.Normalize(now)
	return err
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCommand, fmt.Sprintf(format, args...))
}
