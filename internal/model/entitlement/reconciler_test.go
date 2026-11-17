package entitlement

import (
	"testing"
	"time"
)

func TestAccessUntilUsesGraceAndRevocationDeadlines(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	command := validCommand(now)
	graceUntil := command.PeriodEnd.Add(48 * time.Hour)
	command.Status = StatusGrace
	command.GraceUntil = &graceUntil
	if got := accessUntil(command); !got.Equal(graceUntil) {
		t.Fatalf("grace access deadline = %v, want %v", got, graceUntil)
	}

	revokedAt := command.PeriodStart.Add(24 * time.Hour)
	command.Status = StatusRevoked
	command.PeriodEnd = now.Add(24 * time.Hour)
	command.RevokedAt = &revokedAt
	if got := accessUntil(command); !got.Equal(revokedAt) {
		t.Fatalf("revoked access deadline = %v, want %v", got, revokedAt)
	}
}

func TestProjectedStatusHandlesProviderStateAndTraffic(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	command := validCommand(now)

	status, finishedAt := projectedStatus(command, command.PeriodEnd, now, false, 100, 60, 40)
	if status != 2 || finishedAt == nil {
		t.Fatalf("traffic-exhausted status = %d, %v; want finished", status, finishedAt)
	}

	command.Status = StatusBillingRetry
	status, finishedAt = projectedStatus(command, command.PeriodEnd, now, false, 100, 0, 0)
	if status != 3 || finishedAt == nil {
		t.Fatalf("billing-retry status = %d, %v; want expired", status, finishedAt)
	}

	command.Status = StatusActive
	status, finishedAt = projectedStatus(command, command.PeriodEnd, now, true, 0, 1000, 1000)
	if status != 1 || finishedAt != nil {
		t.Fatalf("unlimited status = %d, %v; want active", status, finishedAt)
	}
}
