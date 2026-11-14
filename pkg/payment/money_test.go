package payment

import "testing"

func TestParseAmount(t *testing.T) {
	cases := []struct {
		value   string
		want    int64
		wantErr bool
	}{
		{value: "0", want: 0},
		{value: "1", want: 100},
		{value: "10", want: 1000},
		{value: "10.1", want: 1010},
		{value: "10.01", want: 1001},
		{value: "0.99", want: 99},
		{value: "1.001", wantErr: true},
		{value: "-1.00", wantErr: true},
		{value: "1e2", wantErr: true},
		{value: " 1.00", wantErr: true},
		{value: "1.00 ", wantErr: true},
		{value: "", wantErr: true},
		{value: ".5", wantErr: true},
		{value: "1.", wantErr: true},
		{value: "abc", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			got, err := ParseAmount(c.value)
			if (err != nil) != c.wantErr {
				t.Fatalf("ParseAmount(%q) error=%v, wantErr=%v", c.value, err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Fatalf("ParseAmount(%q)=%d, want %d", c.value, got, c.want)
			}
		})
	}
}

func TestParseAmountRejectsOverflow(t *testing.T) {
	if _, err := ParseAmount("9223372036854775808"); err == nil {
		t.Fatal("expected overflow to be rejected")
	}
	if _, err := ParseAmount("999999999999999999999"); err == nil {
		t.Fatal("expected oversized input to be rejected")
	}
}
