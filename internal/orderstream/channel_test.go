package orderstream

import "testing"

func TestChannelNamespacesByOrderNo(t *testing.T) {
	cases := []struct {
		orderNo string
		want    string
	}{
		{"202607230001", "order-events:202607230001"},
		{"", "order-events:"},
		// The order number is the identity a client already holds, so it must be
		// used verbatim rather than transformed.
		{"order-with-dashes-and_underscores", "order-events:order-with-dashes-and_underscores"},
	}
	for _, tc := range cases {
		if got := Channel(tc.orderNo); got != tc.want {
			t.Errorf("Channel(%q) = %q, want %q", tc.orderNo, got, tc.want)
		}
	}
	if Channel("a") == Channel("b") {
		t.Fatal("distinct orders must not share a channel")
	}
}
