package subscribe

import (
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONInt64SliceSQLRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input any
		want  JSONInt64Slice
		err   bool
	}{
		{"nil", nil, JSONInt64Slice{}, false},
		{"bytes", []byte(`[7,9007199254740993]`), JSONInt64Slice{7, 9007199254740993}, false},
		{"string", `[11,13]`, JSONInt64Slice{11, 13}, false},
		{"empty", []byte{}, JSONInt64Slice{}, false},
		{"legacy scalar", "17", JSONInt64Slice{}, false},
		{"unsupported value", int64(4), JSONInt64Slice{}, false},
		{"invalid json", []byte(`[1`), JSONInt64Slice{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got JSONInt64Slice
			err := got.Scan(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, tc := range []struct {
		name  string
		value JSONInt64Slice
		want  driver.Value
	}{
		{"empty", JSONInt64Slice{}, "[]"},
		{"values", JSONInt64Slice{5, 8}, []byte(`[5,8]`)},
	} {
		t.Run(tc.name+" value", func(t *testing.T) {
			got, err := tc.value.Value()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
