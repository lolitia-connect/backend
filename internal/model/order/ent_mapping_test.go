package order

import "testing"

func TestDefaultBalancePayment(t *testing.T) {
	payment := defaultBalancePayment()
	if payment == nil {
		t.Fatal("expected balance payment")
	}
	if payment.Id != balancePaymentID {
		t.Fatalf("expected balance payment id %d, got %d", balancePaymentID, payment.Id)
	}
	if payment.Name != "Balance" {
		t.Fatalf("expected balance payment name Balance, got %q", payment.Name)
	}
	if payment.Platform != "balance" {
		t.Fatalf("expected balance payment platform balance, got %q", payment.Platform)
	}
	if payment.Enable == nil || !*payment.Enable {
		t.Fatal("expected balance payment to be enabled")
	}
}

func TestIsBalancePayment(t *testing.T) {
	cases := []struct {
		name string
		data *Details
		want bool
	}{
		{
			name: "nil details",
			data: nil,
			want: false,
		},
		{
			name: "balance id",
			data: &Details{PaymentId: balancePaymentID},
			want: true,
		},
		{
			name: "balance method",
			data: &Details{PaymentId: 1, Method: "balance"},
			want: true,
		},
		{
			name: "normal payment",
			data: &Details{PaymentId: 1, Method: "Stripe"},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBalancePayment(tt.data); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
