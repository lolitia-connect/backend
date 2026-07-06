package tool

import (
	"testing"
	"time"
)

func TestDeepCopyTimeToInt64(t *testing.T) {
	type src struct {
		CreatedAt  time.Time
		FinishedAt *time.Time
	}
	type dst struct {
		CreatedAt  int64
		FinishedAt int64
	}

	finishedAt := time.Date(2026, 7, 6, 12, 0, 0, 123000000, time.UTC)
	var got dst
	DeepCopy(&got, src{
		CreatedAt:  time.Time{},
		FinishedAt: &finishedAt,
	})

	if got.CreatedAt != 0 {
		t.Fatalf("CreatedAt = %d, want 0", got.CreatedAt)
	}
	if got.FinishedAt != finishedAt.UnixMilli() {
		t.Fatalf("FinishedAt = %d, want %d", got.FinishedAt, finishedAt.UnixMilli())
	}
}

func TestDeepCopyNilTimePointerToInt64(t *testing.T) {
	type src struct {
		FinishedAt *time.Time
	}
	type dst struct {
		FinishedAt int64
	}

	var got dst
	DeepCopy(&got, src{})

	if got.FinishedAt != 0 {
		t.Fatalf("FinishedAt = %d, want 0", got.FinishedAt)
	}
}

func TestUnixHelpersZeroTime(t *testing.T) {
	var zero time.Time
	if Unix(zero) != 0 {
		t.Fatalf("Unix(zero) = %d, want 0", Unix(zero))
	}
	if UnixMilli(zero) != 0 {
		t.Fatalf("UnixMilli(zero) = %d, want 0", UnixMilli(zero))
	}
	if UnixPtr(nil) != 0 {
		t.Fatalf("UnixPtr(nil) = %d, want 0", UnixPtr(nil))
	}
	if UnixMilliPtr(&zero) != 0 {
		t.Fatalf("UnixMilliPtr(&zero) = %d, want 0", UnixMilliPtr(&zero))
	}
}

func TestUnixHelpersNonZeroTime(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 123000000, time.UTC)
	if Unix(now) != now.Unix() {
		t.Fatalf("Unix(now) = %d, want %d", Unix(now), now.Unix())
	}
	if UnixMilli(now) != now.UnixMilli() {
		t.Fatalf("UnixMilli(now) = %d, want %d", UnixMilli(now), now.UnixMilli())
	}
	if UnixPtr(&now) != now.Unix() {
		t.Fatalf("UnixPtr(&now) = %d, want %d", UnixPtr(&now), now.Unix())
	}
	if UnixMilliPtr(&now) != now.UnixMilli() {
		t.Fatalf("UnixMilliPtr(&now) = %d, want %d", UnixMilliPtr(&now), now.UnixMilli())
	}
}
