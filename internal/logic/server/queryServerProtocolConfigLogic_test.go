package server

import (
	"slices"
	"testing"

	"github.com/perfect-panel/server/internal/types"
)

func TestFilterServerProtocols(t *testing.T) {
	protocols := []types.Protocol{
		{Type: "vless", Id: "first", Enable: true},
		{Type: "vless", Id: "second", Enable: true},
		{Type: "vless", Id: "disabled", Enable: false},
		{Type: "vmess", Id: "first", Enable: true},
		{Type: "trojan", Id: "third", Enable: true},
	}
	tests := []struct {
		name      string
		requested []string
		want      []string
	}{
		{
			name: "no filter returns all enabled instances",
			want: []string{"vless:first", "vless:second", "vmess:first", "trojan:third"},
		},
		{
			name:      "protocol type returns all matching instances",
			requested: []string{"vless"},
			want:      []string{"vless:first", "vless:second"},
		},
		{
			name:      "instance selector matches both type and id",
			requested: []string{"vless:first"},
			want:      []string{"vless:first"},
		},
		{
			name:      "multiple protocol types",
			requested: []string{"vless", "vmess"},
			want:      []string{"vless:first", "vless:second", "vmess:first"},
		},
		{
			name:      "multiple instance selectors",
			requested: []string{"vless:second", "trojan:third"},
			want:      []string{"vless:second", "trojan:third"},
		},
		{
			name:      "type and instance selectors can be mixed",
			requested: []string{"vless", "trojan:third"},
			want:      []string{"vless:first", "vless:second", "trojan:third"},
		},
		{
			name:      "overlapping selectors do not duplicate instances",
			requested: []string{"vless", "vless:first", "vless"},
			want:      []string{"vless:first", "vless:second"},
		},
		{
			name:      "disabled instance is excluded even when selected",
			requested: []string{"vless:disabled"},
		},
		{
			name:      "unknown selector returns no instances",
			requested: []string{"unknown"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filtered := filterServerProtocols(protocols, test.requested)
			var got []string
			for _, protocol := range filtered {
				got = append(got, protocol.Type+":"+protocol.Id)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("filterServerProtocols() = %v, want %v", got, test.want)
			}
		})
	}
}
