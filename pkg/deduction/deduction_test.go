package deduction

import (
	"math"
	"testing"
	"time"
)

func TestSubscribe_Validate(t *testing.T) {
	tests := []struct {
		name    string
		sub     Subscribe
		wantErr bool
		errType error
	}{
		{
			name: "valid subscription",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 50,
			},
			wantErr: false,
		},
		{
			name: "negative traffic",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        -1000,
				Download:       100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTraffic,
		},
		{
			name: "negative download",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       -100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTraffic,
		},
		{
			name: "download + upload exceeds traffic",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       600,
				Upload:         500,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
		},
		{
			name: "expire time before start time",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(-24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTimeRange,
		},
		{
			name: "invalid deduction ratio - negative",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: -10,
			},
			wantErr: true,
			errType: ErrInvalidDeductionRatio,
		},
		{
			name: "invalid deduction ratio - over 100",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 150,
			},
			wantErr: true,
			errType: ErrInvalidDeductionRatio,
		},
		{
			name: "invalid unit time",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       "InvalidUnit",
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidUnitTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sub.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Subscribe.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("Subscribe.Validate() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestOrder_Validate(t *testing.T) {
	tests := []struct {
		name    string
		order   Order
		wantErr bool
		errType error
	}{
		{
			name:    "valid order",
			order:   Order{Amount: 1000, Quantity: 2},
			wantErr: false,
		},
		{
			name:    "zero quantity",
			order:   Order{Amount: 1000, Quantity: 0},
			wantErr: true,
			errType: ErrInvalidQuantity,
		},
		{
			name:    "negative quantity",
			order:   Order{Amount: 1000, Quantity: -1},
			wantErr: true,
			errType: ErrInvalidQuantity,
		},
		{
			name:    "negative amount",
			order:   Order{Amount: -1000, Quantity: 2},
			wantErr: true,
			errType: ErrInvalidAmount,
		},
		{
			name:    "zero amount is valid",
			order:   Order{Amount: 0, Quantity: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.order.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Order.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && err != tt.errType {
				t.Errorf("Order.Validate() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestSafeMultiply(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr bool
	}{
		{
			name:    "normal multiplication",
			a:       10,
			b:       20,
			want:    200,
			wantErr: false,
		},
		{
			name:    "zero multiplication",
			a:       10,
			b:       0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "negative multiplication",
			a:       -10,
			b:       20,
			want:    -200,
			wantErr: false,
		},
		{
			name:    "overflow case",
			a:       math.MaxInt64,
			b:       2,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large numbers no overflow",
			a:       1000000,
			b:       1000000,
			want:    1000000000000,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeMultiply(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeMultiply() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("safeMultiply() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr bool
	}{
		{
			name:    "normal addition",
			a:       10,
			b:       20,
			want:    30,
			wantErr: false,
		},
		{
			name:    "negative addition",
			a:       -10,
			b:       5,
			want:    -5,
			wantErr: false,
		},
		{
			name:    "overflow case",
			a:       math.MaxInt64,
			b:       1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "underflow case",
			a:       math.MinInt64,
			b:       -1,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeAdd(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeAdd() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("safeAdd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeDivide(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr bool
	}{
		{
			name:    "normal division",
			a:       20,
			b:       10,
			want:    2,
			wantErr: false,
		},
		{
			name:    "division by zero",
			a:       20,
			b:       0,
			want:    0,
			wantErr: true,
		},
		{
			name:    "negative division",
			a:       -20,
			b:       10,
			want:    -2,
			wantErr: false,
		},
		{
			name:    "zero dividend",
			a:       0,
			b:       10,
			want:    0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeDivide(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeDivide() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("safeDivide() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateWeights(t *testing.T) {
	tests := []struct {
		name              string
		deductionRatio    int64
		wantTrafficWeight float64
		wantTimeWeight    float64
	}{
		{
			name:              "zero ratio",
			deductionRatio:    0,
			wantTrafficWeight: 0,
			wantTimeWeight:    0,
		},
		{
			name:              "50% ratio",
			deductionRatio:    50,
			wantTrafficWeight: 0.5,
			wantTimeWeight:    0.5,
		},
		{
			name:              "75% ratio",
			deductionRatio:    75,
			wantTrafficWeight: 0.75,
			wantTimeWeight:    0.25,
		},
		{
			name:              "100% ratio",
			deductionRatio:    100,
			wantTrafficWeight: 1.0,
			wantTimeWeight:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTrafficWeight, gotTimeWeight := calculateWeights(tt.deductionRatio)
			if gotTrafficWeight != tt.wantTrafficWeight {
				t.Errorf("calculateWeights() trafficWeight = %v, want %v", gotTrafficWeight, tt.wantTrafficWeight)
			}
			if gotTimeWeight != tt.wantTimeWeight {
				t.Errorf("calculateWeights() timeWeight = %v, want %v", gotTimeWeight, tt.wantTimeWeight)
			}
		})
	}
}

func TestCalculateProportionalAmount(t *testing.T) {
	tests := []struct {
		name      string
		unitPrice int64
		remaining int64
		total     int64
		want      int64
		wantErr   bool
	}{
		{
			name:      "normal calculation",
			unitPrice: 100,
			remaining: 50,
			total:     100,
			want:      50,
			wantErr:   false,
		},
		{
			name:      "zero total",
			unitPrice: 100,
			remaining: 50,
			total:     0,
			want:      0,
			wantErr:   false,
		},
		{
			name:      "zero remaining",
			unitPrice: 100,
			remaining: 0,
			total:     100,
			want:      0,
			wantErr:   false,
		},
		{
			name:      "quarter remaining",
			unitPrice: 200,
			remaining: 25,
			total:     100,
			want:      50,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateProportionalAmount(tt.unitPrice, tt.remaining, tt.total)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateProportionalAmount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("calculateProportionalAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateNoLimitAmount(t *testing.T) {
	tests := []struct {
		name    string
		sub     Subscribe
		order   Order
		want    int64
		wantErr bool
	}{
		{
			name: "normal no limit calculation",
			sub: Subscribe{
				Traffic:  1000,
				Download: 300,
				Upload:   200,
			},
			order: Order{
				Amount: 1000,
			},
			want:    500, // (1000 - 300 - 200) / 1000 * 1000 = 500
			wantErr: false,
		},
		{
			name: "zero traffic",
			sub: Subscribe{
				Traffic:          0,
				TrafficUnlimited: true,
				Download:         0,
				Upload:           0,
			},
			order: Order{
				Amount: 1000,
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "overused traffic",
			sub: Subscribe{
				Traffic:  1000,
				Download: 600,
				Upload:   500,
			},
			order: Order{
				Amount: 1000,
			},
			want:    0, // usedTraffic would be negative, clamped to 0
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateNoLimitAmount(tt.sub, tt.order)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateNoLimitAmount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("calculateNoLimitAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateRemainingAmount(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		sub     Subscribe
		order   Order
		wantErr bool
	}{
		{
			name: "valid no limit subscription",
			sub: Subscribe{
				StartTime:      now.Add(-24 * time.Hour),
				ExpireTime:     now.Add(24 * time.Hour),
				Traffic:        1000,
				Download:       300,
				Upload:         200,
				UnitTime:       UnitTimeNoLimit,
				ResetCycle:     ResetCycleNone,
				DeductionRatio: 0,
			},
			order: Order{
				Amount:   1000,
				Quantity: 1,
			},
			wantErr: false,
		},
		{
			name: "invalid subscription",
			sub: Subscribe{
				StartTime:      now,
				ExpireTime:     now.Add(-24 * time.Hour), // Invalid: expire before start
				Traffic:        1000,
				Download:       300,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 0,
			},
			order: Order{
				Amount:   1000,
				Quantity: 1,
			},
			wantErr: true,
		},
		{
			name: "invalid order",
			sub: Subscribe{
				StartTime:      now.Add(-24 * time.Hour),
				ExpireTime:     now.Add(24 * time.Hour),
				Traffic:        1000,
				Download:       300,
				Upload:         200,
				UnitTime:       UnitTimeMonth,
				DeductionRatio: 0,
			},
			order: Order{
				Amount:   1000,
				Quantity: 0, // Invalid: zero quantity
			},
			wantErr: true,
		},
		{
			name: "no limit with reset cycle",
			sub: Subscribe{
				StartTime:      now.Add(-24 * time.Hour),
				ExpireTime:     now.Add(24 * time.Hour),
				Traffic:        1000,
				Download:       300,
				Upload:         200,
				UnitTime:       UnitTimeNoLimit,
				ResetCycle:     ResetCycleMonthly, // Should return 0
				DeductionRatio: 0,
			},
			order: Order{
				Amount:   1000,
				Quantity: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateRemainingAmount(tt.sub, tt.order, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateRemainingAmount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateRemainingAmount_CrossMonthYear(t *testing.T) {
	tests := []struct {
		name       string
		startTime  time.Time
		expireTime time.Time
		now        time.Time
		traffic    int64
		download   int64
		upload     int64
		wantMin    int64
		wantMax    int64
	}{
		// === 同月 ===
		{
			name:       "同月: 6/1买, 6/15算, 7/1到期 (半月)",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 533, wantMax: 533, // 16天/30天 × 1000 = 533
		},
		{
			name:       "同月: 6/1买, 6/1算, 7/1到期 (刚买)",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 1000, wantMax: 1000, // 30天/30天 × 1000 = 1000
		},
		{
			name:       "同月: 6/1买, 6/30 23:59算, 7/1到期 (最后几分钟)",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 30, 23, 59, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 0, wantMax: 0, // 不足1天, 截断为0
		},

		// === 跨月 ===
		{
			name:       "跨月: 6/15买, 7/15算, 8/15到期 (半月)",
			startTime:  time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 8, 15, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 508, wantMax: 508, // 31天/61天 × 1000 = 508
		},
		{
			name:       "跨月: 6/15买, 6/30算, 8/15到期 (月末)",
			startTime:  time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 8, 15, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 30, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 754, wantMax: 754, // 46天/61天 × 1000 = 754
		},
		{
			name:       "跨月: 6/15买, 7/1算, 8/15到期 (月初)",
			startTime:  time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 8, 15, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 737, wantMax: 737, // 45天/61天 × 1000 = 737
		},

		// === 跨年 ===
		{
			name:       "跨年: 11/15买, 12/15算, 次年1/15到期 (半月)",
			startTime:  time.Date(2025, 11, 15, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local),
			now:        time.Date(2025, 12, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 508, wantMax: 508, // 31天/61天 × 1000 = 508
		},
		{
			name:       "跨年: 12/31买, 1/1算, 1/31到期 (跨年第二天)",
			startTime:  time.Date(2025, 12, 31, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 967, wantMax: 967, // 30天/31天 × 1000 = 967
		},
		{
			name:       "跨年: 12/31买, 12/31算, 1/31到期 (刚买)",
			startTime:  time.Date(2025, 12, 31, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local),
			now:        time.Date(2025, 12, 31, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 1000, wantMax: 1000, // 31天/31天 × 1000 = 1000
		},
		{
			name:       "跨年: 12/31买, 1/30算, 1/31到期 (最后一天)",
			startTime:  time.Date(2025, 12, 31, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 1, 30, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 32, wantMax: 32, // 1天/31天 × 1000 = 32
		},

		// === 2月(非闰年) ===
		{
			name:       "2月非闰: 1/31买, 2/15算, 2/28到期",
			startTime:  time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 2, 28, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 2, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 430, wantMax: 480,
		},

		// === 2月(闰年) ===
		{
			name:       "2月闰年: 1/31买, 2/15算, 2/29到期",
			startTime:  time.Date(2028, 1, 31, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2028, 2, 29, 10, 0, 0, 0, time.Local),
			now:        time.Date(2028, 2, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 440, wantMax: 490,
		},

		// === 已过期 ===
		{
			name:       "已过期: 6/1买, 7/15算, 7/1到期",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 0, upload: 0,
			wantMin: 0, wantMax: 0,
		},

		// === 流量限制 ===
		{
			name:       "流量用尽: 即使时间剩余很多, 流量用完也退0",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 2, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 500, upload: 500,
			wantMin: 0, wantMax: 0,
		},
		{
			name:       "流量用一半: 时间和流量各退一半, 取min",
			startTime:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local),
			expireTime: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
			now:        time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local),
			traffic:    1000, download: 250, upload: 250,
			wantMin: 499, wantMax: 501,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := Subscribe{
				StartTime:        tt.startTime,
				ExpireTime:       tt.expireTime,
				Traffic:          tt.traffic,
				Download:         tt.download,
				Upload:           tt.upload,
				TrafficUnlimited: tt.traffic == 0,
				UnitTime:         UnitTimeMonth,
				ResetCycle:       ResetCycleNone,
				DeductionRatio:   0,
			}
			order := Order{Amount: 1000, Quantity: 1}

			got, err := CalculateRemainingAmount(sub, order, tt.now)
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}
			t.Logf("got=%d, want=[%d,%d]", got, tt.wantMin, tt.wantMax)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("got=%d, want=[%d,%d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateRemainingAmount_MonthResetCycleNone(t *testing.T) {
	// Use a fixed time to avoid time.Now() drift between subscription creation and calculation
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	t.Logf("Fixed time: %v, day: %d", now, now.Day())

	tests := []struct {
		name         string
		startOffset  time.Duration
		expireOffset time.Duration
		want         int64
	}{
		{
			name:         "just purchased today",
			startOffset:  0,
			expireOffset: 30 * 24 * time.Hour,
			want:         1000,
		},
		{
			name:         "purchased 15 days ago",
			startOffset:  -15 * 24 * time.Hour,
			expireOffset: 15 * 24 * time.Hour,
			want:         500,
		},
		{
			name:         "purchased 29 days ago, 1 day left",
			startOffset:  -29 * 24 * time.Hour,
			expireOffset: 1 * 24 * time.Hour,
			want:         33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := Subscribe{
				StartTime:        now.Add(tt.startOffset),
				ExpireTime:       now.Add(tt.expireOffset),
				Traffic:          1000,
				Download:         0,
				Upload:           0,
				TrafficUnlimited: true,
				UnitTime:         UnitTimeMonth,
				ResetCycle:       ResetCycleNone,
				DeductionRatio:   0,
			}
			order := Order{
				Amount:   1000,
				Quantity: 1,
			}

			// Manually calculate expected result to avoid time.Now() drift
			remainingDays := float64(tt.expireOffset) / float64(24*time.Hour)
			totalDays := float64(tt.expireOffset-tt.startOffset) / float64(24*time.Hour)
			expected := int64(float64(order.Amount) / float64(order.Quantity) * remainingDays / totalDays)
			t.Logf("remaining value: want=%d (remainingDays=%.1f, totalDays=%.1f)", expected, remainingDays, totalDays)

			got, err := CalculateRemainingAmount(sub, order, now)
			if err != nil {
				t.Errorf("CalculateRemainingAmount() error = %v", err)
				return
			}
			t.Logf("remaining value: got=%d", got)
			// Allow ±1 difference due to int64 truncation
			diff := got - expected
			if diff < -1 || diff > 1 {
				t.Errorf("CalculateRemainingAmount() = %d, want ~%d", got, expected)
			}
		})
	}
}

func TestCalculateRemainingAmount_NoLimitWithResetCycle(t *testing.T) {
	now := time.Now()
	sub := Subscribe{
		StartTime:      now.Add(-24 * time.Hour),
		ExpireTime:     now.Add(24 * time.Hour),
		Traffic:        1000,
		Download:       300,
		Upload:         200,
		UnitTime:       UnitTimeNoLimit,
		ResetCycle:     ResetCycleMonthly,
		DeductionRatio: 0,
	}
	order := Order{
		Amount:   1000,
		Quantity: 1,
	}

	got, err := CalculateRemainingAmount(sub, order, now)
	if err != nil {
		t.Errorf("CalculateRemainingAmount() error = %v", err)
		return
	}
	// NoLimit + ResetCycle!=0: has reset cycle, should calculate based on remaining traffic
	// remaining traffic = 1000 - 300 - 200 = 500, result = 500/1000 * 1000 = 500
	if got != 500 {
		t.Errorf("CalculateRemainingAmount() = %v, want 500", got)
	}
}

func TestCalculateRemainingAmount_NoLimitNoResetCycle(t *testing.T) {
	now := time.Now()
	sub := Subscribe{
		StartTime:      now.Add(-24 * time.Hour),
		ExpireTime:     now.Add(24 * time.Hour),
		Traffic:        1000,
		Download:       300,
		Upload:         200,
		UnitTime:       UnitTimeNoLimit,
		ResetCycle:     ResetCycleNone,
		DeductionRatio: 0,
	}
	order := Order{
		Amount:   1000,
		Quantity: 1,
	}

	got, err := CalculateRemainingAmount(sub, order, now)
	if err != nil {
		t.Errorf("CalculateRemainingAmount() error = %v", err)
		return
	}
	// NoLimit + ResetCycle==0: truly unlimited, cannot cancel
	if got != 0 {
		t.Errorf("CalculateRemainingAmount() = %v, want 0", got)
	}
}

// Benchmark tests
func BenchmarkCalculateRemainingAmount(b *testing.B) {
	now := time.Now()
	sub := Subscribe{
		StartTime:      now.Add(-24 * time.Hour),
		ExpireTime:     now.Add(24 * time.Hour),
		Traffic:        1000,
		Download:       300,
		Upload:         200,
		UnitTime:       UnitTimeMonth,
		ResetCycle:     ResetCycleNone,
		DeductionRatio: 50,
	}
	order := Order{
		Amount:   1000,
		Quantity: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CalculateRemainingAmount(sub, order, now)
	}
}

func BenchmarkSafeMultiply(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = safeMultiply(12345, 67890)
	}
}
