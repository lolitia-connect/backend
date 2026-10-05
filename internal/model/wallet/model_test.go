package wallet

import "testing"

func TestWalletAvailable(t *testing.T) {
	cases := []struct {
		name   string
		wallet Wallet
		want   int64
	}{
		{name: "empty wallet", wallet: Wallet{}, want: 0},
		{name: "balance only", wallet: Wallet{Balance: 1500}, want: 1500},
		{name: "gift only", wallet: Wallet{GiftAmount: 250}, want: 250},
		{name: "balance plus gift", wallet: Wallet{Balance: 1500, GiftAmount: 250}, want: 1750},
		{name: "commission is not spendable", wallet: Wallet{Balance: 10, GiftAmount: 20, Commission: 9999}, want: 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.wallet.Available(); got != tc.want {
				t.Fatalf("Available() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWalletReserveSpendsGiftFirst(t *testing.T) {
	cases := []struct {
		name          string
		wallet        Wallet
		amount        int64
		wantGift      int64
		wantRemainder int64
	}{
		{
			name:          "gift covers the whole amount",
			wallet:        Wallet{Balance: 1000, GiftAmount: 500},
			amount:        300,
			wantGift:      300,
			wantRemainder: 0,
		},
		{
			name:          "gift covers the amount exactly",
			wallet:        Wallet{Balance: 1000, GiftAmount: 300},
			amount:        300,
			wantGift:      300,
			wantRemainder: 0,
		},
		{
			name:          "gift is exhausted and cash pays the rest",
			wallet:        Wallet{Balance: 1000, GiftAmount: 200},
			amount:        500,
			wantGift:      200,
			wantRemainder: 300,
		},
		{
			name:          "no gift credit",
			wallet:        Wallet{Balance: 1000},
			amount:        500,
			wantGift:      0,
			wantRemainder: 500,
		},
		{
			name:          "zero amount reserves nothing",
			wallet:        Wallet{Balance: 1000, GiftAmount: 500},
			amount:        0,
			wantGift:      0,
			wantRemainder: 0,
		},
		{
			name:          "negative amount reserves nothing",
			wallet:        Wallet{Balance: 1000, GiftAmount: 500},
			amount:        -50,
			wantGift:      0,
			wantRemainder: -50,
		},
		{
			name:          "empty wallet leaves the amount to the caller",
			wallet:        Wallet{},
			amount:        500,
			wantGift:      0,
			wantRemainder: 500,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gift, remainder := tc.wallet.Reserve(tc.amount)
			if gift != tc.wantGift || remainder != tc.wantRemainder {
				t.Fatalf("Reserve(%d) = (%d, %d), want (%d, %d)",
					tc.amount, gift, remainder, tc.wantGift, tc.wantRemainder)
			}
			if tc.amount > 0 && gift+remainder != tc.amount {
				t.Fatalf("Reserve(%d) lost money: %d + %d != %d",
					tc.amount, gift, remainder, tc.amount)
			}
			if gift > tc.wallet.GiftAmount {
				t.Fatalf("Reserve(%d) spent %d of %d gift credit", tc.amount, gift, tc.wallet.GiftAmount)
			}
		})
	}
}

func TestWalletReserveNeverOverdrawsGift(t *testing.T) {
	wallet := Wallet{Balance: 100, GiftAmount: 700}
	gift, remainder := wallet.Reserve(900)
	if gift != 700 {
		t.Fatalf("gift = %d, want 700", gift)
	}
	if remainder != 200 {
		t.Fatalf("remainder = %d, want 200", remainder)
	}
	// The caller has to notice it cannot pay the cash part.
	if wallet.Balance < remainder {
		// expected: the wallet is short of 100 on the cash side
		return
	}
	t.Fatal("expected the cash remainder to exceed the balance")
}
