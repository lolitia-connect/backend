package github

import "testing"

func TestSelectVerifiedEmail(t *testing.T) {
	cases := []struct {
		name   string
		emails []EmailInfo
		want   string
		wantOK bool
	}{
		{
			name:   "empty list",
			emails: nil,
			wantOK: false,
		},
		{
			name: "prefers primary verified",
			emails: []EmailInfo{
				{Email: "secondary@example.com", Verified: true},
				{Email: "primary@example.com", Primary: true, Verified: true},
			},
			want:   "primary@example.com",
			wantOK: true,
		},
		{
			name: "primary but unverified is skipped in favor of verified",
			emails: []EmailInfo{
				{Email: "primary@example.com", Primary: true, Verified: false},
				{Email: "verified@example.com", Verified: true},
			},
			want:   "verified@example.com",
			wantOK: true,
		},
		{
			name: "falls back to first verified when none primary",
			emails: []EmailInfo{
				{Email: "unverified@example.com"},
				{Email: "verified@example.com", Verified: true},
			},
			want:   "verified@example.com",
			wantOK: true,
		},
		{
			name: "rejects when nothing verified",
			emails: []EmailInfo{
				{Email: "primary@example.com", Primary: true},
				{Email: "other@example.com"},
			},
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := SelectVerifiedEmail(c.emails)
			if ok != c.wantOK || got != c.want {
				t.Fatalf("SelectVerifiedEmail() = (%q, %v), want (%q, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestNewRequestsEmailScope(t *testing.T) {
	client := New(&Config{ClientID: "id", ClientSecret: "secret"})
	scopes := map[string]bool{}
	for _, s := range client.Scopes {
		scopes[s] = true
	}
	if !scopes["user:email"] {
		t.Fatal("github client must request user:email scope to read verified emails")
	}
}
