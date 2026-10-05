package subscribe

import (
	"reflect"
	"testing"
)

func TestMergeParams(t *testing.T) {
	cases := []struct {
		name      string
		defaults  map[string]string
		requested map[string]string
		want      map[string]string
	}{
		{
			name:      "no defaults returns the request untouched",
			requested: map[string]string{"mode": "global"},
			want:      map[string]string{"mode": "global"},
		},
		{
			name:     "defaults apply when the request carries nothing",
			defaults: map[string]string{"mode": "rule", "emoji": "1"},
			want:     map[string]string{"mode": "rule", "emoji": "1"},
		},
		{
			name:      "the request wins on a shared key",
			defaults:  map[string]string{"mode": "rule", "emoji": "1"},
			requested: map[string]string{"mode": "global"},
			want:      map[string]string{"mode": "global", "emoji": "1"},
		},
		{
			name:      "an empty request value still overrides",
			defaults:  map[string]string{"emoji": "1"},
			requested: map[string]string{"emoji": ""},
			want:      map[string]string{"emoji": ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeParams(tc.defaults, tc.requested)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeParams(%v, %v) = %v, want %v", tc.defaults, tc.requested, got, tc.want)
			}
		})
	}
}

// The defaults map comes from the client application and is reused across
// requests, so merging must not write into it.
func TestMergeParamsDoesNotMutateDefaults(t *testing.T) {
	defaults := map[string]string{"mode": "rule"}
	mergeParams(defaults, map[string]string{"mode": "global", "emoji": "1"})

	if want := map[string]string{"mode": "rule"}; !reflect.DeepEqual(defaults, want) {
		t.Fatalf("defaults = %v after merging, want %v", defaults, want)
	}
}

// mergeParams returns the requested map as-is when there is nothing to layer,
// which keeps the common no-defaults path allocation free.
func TestMergeParamsReturnsRequestWhenNoDefaults(t *testing.T) {
	requested := map[string]string{"mode": "global"}
	if got := mergeParams(nil, requested); reflect.ValueOf(got).Pointer() != reflect.ValueOf(requested).Pointer() {
		t.Fatalf("mergeParams(nil, requested) = %v, want the same map back", got)
	}
}
