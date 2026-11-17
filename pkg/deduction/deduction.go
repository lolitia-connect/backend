// Package deduction provides functionality for calculating remaining amounts
// in subscription billing systems, supporting various time units and traffic-based calculations.
package deduction

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	// Time unit constants for subscription billing
	UnitTimeNoLimit = "NoLimit" // Unlimited time subscription
	UnitTimeYear    = "Year"    // Annual subscription
	UnitTimeMonth   = "Month"   // Monthly subscription
	UnitTimeDay     = "Day"     // Daily subscription
	UnitTimeHour    = "Hour"    // Hourly subscription
	UnitTimeMinute  = "Minute"  // Per-minute subscription

	// Reset cycle constants for traffic resets
	ResetCycleNone    = 0 // No reset cycle
	ResetCycle1st     = 1 // Reset on 1st of each month
	ResetCycleMonthly = 2 // Reset monthly based on start date
	ResetCycleYear    = 3 // Reset yearly based on start date

	// Safety limits for overflow protection
	maxInt64 = math.MaxInt64
	minInt64 = math.MinInt64
)

// Error definitions for validation and calculation failures
var (
	ErrInvalidQuantity       = errors.New("order quantity cannot be zero or negative")
	ErrInvalidAmount         = errors.New("order amount cannot be negative")
	ErrInvalidTraffic        = errors.New("traffic values cannot be negative")
	ErrInvalidTimeRange      = errors.New("expire time must be after start time")
	ErrInvalidUnitTime       = errors.New("invalid unit time")
	ErrInvalidDeductionRatio = errors.New("deduction ratio must be between 0 and 100")
	ErrOverflow              = errors.New("calculation overflow")
)

// Subscribe represents a subscription with time and traffic limits
type Subscribe struct {
	StartTime        time.Time // Subscription start time
	ExpireTime       time.Time // Subscription expiration time
	Traffic          int64     // Total traffic allowance in bytes
	TrafficUnlimited bool      // Whether traffic is unlimited
	Download         int64     // Downloaded traffic in bytes
	Upload           int64     // Uploaded traffic in bytes
	UnitTime         string    // Time unit for billing (Year, Month, Day, etc.)
	UnitPrice        int64     // Price per unit time
	ResetCycle       int64     // Traffic reset cycle
	DeductionRatio   int64     // Deduction ratio for weighted calculations (0-100)
}

// Order represents a purchase order for subscription calculation
type Order struct {
	Amount   int64 // Total order amount
	Quantity int64 // Order quantity
}

// Validate checks if the Subscribe struct contains valid data
func (s *Subscribe) Validate() error {
	if s.Traffic < 0 || s.Download < 0 || s.Upload < 0 {
		return ErrInvalidTraffic
	}

	if !s.noTimeLimit() && !s.ExpireTime.After(s.StartTime) {
		return ErrInvalidTimeRange
	}

	if s.DeductionRatio < 0 || s.DeductionRatio > 100 {
		return ErrInvalidDeductionRatio
	}

	validUnitTimes := []string{UnitTimeNoLimit, UnitTimeYear, UnitTimeMonth, UnitTimeDay, UnitTimeHour, UnitTimeMinute}
	valid := false
	for _, ut := range validUnitTimes {
		if s.UnitTime == ut {
			valid = true
			break
		}
	}
	if !valid {
		return ErrInvalidUnitTime
	}

	return nil
}

// Validate checks if the Order struct contains valid data
func (o *Order) Validate() error {
	if o.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	if o.Amount < 0 {
		return ErrInvalidAmount
	}
	return nil
}

// safeMultiply performs multiplication with overflow protection
func safeMultiply(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}

	if a > 0 && b > 0 {
		if a > maxInt64/b {
			return 0, ErrOverflow
		}
	} else if a < 0 && b < 0 {
		if a < maxInt64/b {
			return 0, ErrOverflow
		}
	} else {
		if (a > 0 && b < minInt64/a) || (a < 0 && b > minInt64/a) {
			return 0, ErrOverflow
		}
	}

	return a * b, nil
}

// safeAdd performs addition with overflow protection
func safeAdd(a, b int64) (int64, error) {
	if (b > 0 && a > maxInt64-b) || (b < 0 && a < minInt64-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// safeDivide performs division with zero-division protection
func safeDivide(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

// CalculateRemainingAmount calculates the remaining refund amount for a subscription
// based on unused time and traffic. Returns the amount and any calculation errors.
func CalculateRemainingAmount(sub Subscribe, order Order, now time.Time) (int64, error) {
	if err := sub.Validate(); err != nil {
		return 0, fmt.Errorf("invalid subscription: %w", err)
	}

	if err := order.Validate(); err != nil {
		return 0, fmt.Errorf("invalid order: %w", err)
	}

	if sub.noTimeLimit() {
		if sub.ResetCycle != ResetCycleNone {
			return 0, nil
		}
		return calculateNoLimitAmount(sub, order)
	}
	if order.Amount == 0 || !now.Before(sub.ExpireTime) {
		return 0, nil
	}
	if now.Before(sub.StartTime) {
		now = sub.StartTime
	}
	cycleStart, cycleEnd := currentResetCycle(sub, now)
	remaining := float64(sub.ExpireTime.Sub(cycleEnd))
	if cycle := cycleEnd.Sub(cycleStart); cycle > 0 {
		timeShare := float64(cycleEnd.Sub(now)) / float64(cycle)
		share := timeShare
		if !sub.TrafficUnlimited && sub.Traffic > 0 {
			trafficShare := unusedTrafficShare(sub)
			share = math.Min(timeShare, trafficShare)
			if sub.DeductionRatio != 0 {
				trafficWeight, timeWeight := calculateWeights(sub.DeductionRatio)
				share = trafficWeight*trafficShare + timeWeight*timeShare
			}
		}
		remaining += float64(cycle) * share
	}
	return refundProportion(order.Amount, remaining/float64(sub.ExpireTime.Sub(sub.StartTime))), nil
}

func (s Subscribe) noTimeLimit() bool {
	return s.UnitTime == UnitTimeNoLimit || s.ExpireTime.Before(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC))
}

func unusedTrafficShare(sub Subscribe) float64 {
	if sub.Traffic <= 0 || sub.Download >= sub.Traffic {
		return 0
	}
	remaining := sub.Traffic - sub.Download
	if sub.Upload >= remaining {
		return 0
	}
	return float64(remaining-sub.Upload) / float64(sub.Traffic)
}

func refundProportion(amount int64, share float64) int64 {
	if math.IsNaN(share) || share <= 0 {
		return 0
	}
	if share >= 1 {
		return amount
	}
	result := float64(amount) * share
	if result >= float64(amount) {
		return amount
	}
	return int64(result)
}

func currentResetCycle(sub Subscribe, now time.Time) (time.Time, time.Time) {
	start := sub.StartTime.In(now.Location())
	resetDate := func(year int, month time.Month, day int) time.Time {
		first := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
		return time.Date(first.Year(), first.Month(), min(day, first.AddDate(0, 1, -1).Day()), 0, 0, 0, 0, now.Location())
	}
	var from, to time.Time
	switch sub.ResetCycle {
	case ResetCycle1st:
		from = resetDate(now.Year(), now.Month(), 1)
		to = from.AddDate(0, 1, 0)
	case ResetCycleMonthly:
		from = resetDate(now.Year(), now.Month(), start.Day())
		if from.After(now) {
			from = resetDate(now.Year(), now.Month()-1, start.Day())
		}
		to = resetDate(from.Year(), from.Month()+1, start.Day())
	case ResetCycleYear:
		from = resetDate(now.Year(), start.Month(), start.Day())
		if from.After(now) {
			from = resetDate(now.Year()-1, start.Month(), start.Day())
		}
		to = resetDate(from.Year()+1, start.Month(), start.Day())
	default:
		return sub.StartTime, sub.ExpireTime
	}
	if from.Before(sub.StartTime) {
		from = sub.StartTime
	}
	if to.After(sub.ExpireTime) {
		to = sub.ExpireTime
	}
	return from, to
}

// calculateNoLimitAmount calculates refund amount for unlimited time subscriptions
// based on unused traffic only
func calculateNoLimitAmount(sub Subscribe, order Order) (int64, error) {
	if sub.TrafficUnlimited || sub.Traffic == 0 {
		return 0, nil
	}
	return refundProportion(order.Amount, unusedTrafficShare(sub)), nil
}

// calculateRemainingUnitTimeAmount calculates the remaining amount based on
// both time and traffic usage, applying deduction ratios when specified
func calculateRemainingUnitTimeAmount(sub Subscribe, now time.Time) (int64, error) {
	trafficWeight, timeWeight := calculateWeights(sub.DeductionRatio)
	remainingDays, totalDays := getRemainingAndTotalDays(sub, now)

	if totalDays == 0 {
		return 0, nil
	}

	remainingTraffic := sub.Traffic - sub.Download - sub.Upload
	if remainingTraffic < 0 {
		remainingTraffic = 0
	}

	remainingTimeAmount, err := calculateProportionalAmount(sub.UnitPrice, remainingDays, totalDays)
	if err != nil {
		return 0, fmt.Errorf("time amount calculation failed: %w", err)
	}

	if sub.TrafficUnlimited {
		return remainingTimeAmount, nil
	}

	remainingTrafficAmount, err := calculateProportionalAmount(sub.UnitPrice, remainingTraffic, sub.Traffic)
	if err != nil {
		return 0, fmt.Errorf("traffic amount calculation failed: %w", err)
	}

	if sub.DeductionRatio != 0 {
		return calculateWeightedAmount(sub.UnitPrice, remainingTraffic, sub.Traffic, remainingDays, totalDays, trafficWeight, timeWeight)
	}

	return min(remainingTimeAmount, remainingTrafficAmount), nil
}

// calculateWeights converts deduction ratio to traffic and time weights
// for weighted calculations
func calculateWeights(deductionRatio int64) (float64, float64) {
	if deductionRatio == 0 {
		return 0, 0
	}
	trafficWeight := float64(deductionRatio) / 100
	timeWeight := 1 - trafficWeight
	return trafficWeight, timeWeight
}

// getRemainingAndTotalDays calculates remaining and total days based on
// the subscription's actual start and expire times.
// ResetCycle controls traffic reset frequency, NOT the refund calculation period.
func getRemainingAndTotalDays(sub Subscribe, now time.Time) (int64, int64) {
	remaining := sub.ExpireTime.Sub(now).Hours() / 24
	total := sub.ExpireTime.Sub(sub.StartTime).Hours() / 24
	if remaining < 0 {
		remaining = 0
	}
	if total < 0 {
		total = 0
	}
	return int64(remaining), int64(total)
}

// calculateWeightedAmount applies weighted calculation combining both time and traffic
// remaining ratios based on the specified weights
func calculateWeightedAmount(unitPrice, remainingTraffic, totalTraffic, remainingDays, totalDays int64, trafficWeight, timeWeight float64) (int64, error) {
	if totalDays == 0 || totalTraffic == 0 {
		return 0, nil
	}

	remainingTimeRatio := float64(remainingDays) / float64(totalDays)
	remainingTrafficRatio := float64(remainingTraffic) / float64(totalTraffic)
	weightedRemainingRatio := (timeWeight * remainingTimeRatio) + (trafficWeight * remainingTrafficRatio)

	result := float64(unitPrice) * weightedRemainingRatio
	if result > float64(maxInt64) || result < float64(minInt64) {
		return 0, ErrOverflow
	}

	return int64(result), nil
}

// calculateProportionalAmount calculates proportional amount based on
// remaining vs total ratio with overflow protection
func calculateProportionalAmount(unitPrice, remaining, total int64) (int64, error) {
	if total == 0 {
		return 0, nil
	}

	result := float64(unitPrice) * (float64(remaining) / float64(total))
	if result > float64(maxInt64) || result < float64(minInt64) {
		return 0, ErrOverflow
	}

	return int64(result), nil
}
