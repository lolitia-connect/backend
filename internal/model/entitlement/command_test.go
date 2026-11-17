package entitlement

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validCommand(now time.Time) ReconcileEntitlementCommand {
	return ReconcileEntitlementCommand{
		Source:          SourceApple,
		Scope:           "production",
		SubscriptionKey: "sub-123",
		TransactionKey:  "txn-123",
		UserID:          10,
		PlanID:          20,
		OrderID:         30,
		Revision:        1,
		Mode:            ModeAutoRenewable,
		BillingInterval: BillingIntervalMonth,
		Status:          StatusActive,
		PeriodStart:     now.Add(-time.Hour),
		PeriodEnd:       now.Add(30 * 24 * time.Hour),
		AutoRenew:       true,
	}
}

func TestNormalizeTrimsBoundedIdentityFields(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	command := validCommand(now)
	command.Scope = " production "
	command.SubscriptionKey = " sub-123 "
	command.TransactionKey = " txn-123 "

	normalized, err := command.Normalize(now)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if normalized.Scope != "production" || normalized.SubscriptionKey != "sub-123" || normalized.TransactionKey != "txn-123" {
		t.Fatalf("identity fields were not trimmed: %+v", normalized)
	}
}

func TestNormalizeRejectsInvalidStateCombinations(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*ReconcileEntitlementCommand)
	}{
		{name: "future start", mutate: func(command *ReconcileEntitlementCommand) { command.PeriodStart = now.Add(time.Minute) }},
		{name: "future inactive deadline", mutate: func(command *ReconcileEntitlementCommand) { command.Status = StatusExpired }},
		{name: "missing grace deadline", mutate: func(command *ReconcileEntitlementCommand) { command.Status = StatusGrace }},
		{name: "missing revoked timestamp", mutate: func(command *ReconcileEntitlementCommand) {
			command.Status = StatusRevoked
			command.PeriodEnd = now.Add(-time.Minute)
		}},
		{name: "grace deadline on active", mutate: func(command *ReconcileEntitlementCommand) {
			graceUntil := now.Add(31 * 24 * time.Hour)
			command.GraceUntil = &graceUntil
		}},
		{name: "revocation timestamp on active", mutate: func(command *ReconcileEntitlementCommand) {
			revokedAt := now.Add(-time.Minute)
			command.RevokedAt = &revokedAt
		}},
		{name: "future revocation", mutate: func(command *ReconcileEntitlementCommand) {
			revokedAt := now.Add(time.Minute)
			command.RevokedAt = &revokedAt
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := validCommand(now)
			test.mutate(&command)
			if err := command.Validate(now); !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("Validate() error = %v, want ErrInvalidCommand", err)
			}
		})
	}
}

func TestNormalizeRejectsOversizedIdentity(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	command := validCommand(now)
	command.Scope = strings.Repeat("x", 256)
	if err := command.Validate(now); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("oversized scope error = %v, want ErrInvalidCommand", err)
	}
}
