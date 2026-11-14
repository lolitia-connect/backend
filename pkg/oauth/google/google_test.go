package google

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"

	"golang.org/x/oauth2"
)

func TestGoogleOAuth(t *testing.T) {
	t.Skipf("Skip TestGoogleOAuth test")
	http.HandleFunc("/", handleMain)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/auth", handleCallback)
	http.HandleFunc("/user", handleAuth)

	fmt.Println("Server is running on http://localhost:3001")
	log.Fatal(http.ListenAndServe(":3001", nil))
}

func handleMain(w http.ResponseWriter, r *http.Request) {
	html := `<html>
		<body>
			<a href="/login">Log in with Google</a>
		</body>
	</html>`
	fmt.Fprint(w, html)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	oauthConfig := New(&Config{
		ClientID:     "",
		ClientSecret: "",
		RedirectURL:  "http://localhost:3001/auth",
	})
	url := oauthConfig.AuthCodeURL("randomstate", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("state") != "randomstate" {
		http.Error(w, "State is invalid", http.StatusBadRequest)
		return
	}

	log.Printf("url: %v", r.URL)

	oauthConfig := New(&Config{
		ClientID:     "",
		ClientSecret: "Key",
		RedirectURL:  "http://localhost:3001/auth",
	})
	code := r.FormValue("code")
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?token="+token.AccessToken, http.StatusTemporaryRedirect)
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	client := New(&Config{
		ClientID:     "Id",
		ClientSecret: "Key",
		RedirectURL:  "http://localhost:3001/auth",
	})
	userInfo, err := client.GetUserInfo(token)
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "Hello, %s", userInfo.Name)
}

func TestParseVerifiedEmail(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string false", "false", false},
		{"string garbage", "yes", false},
		{"nil", nil, false},
		{"number", float64(1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseVerifiedEmail(c.input); got != c.want {
				t.Fatalf("parseVerifiedEmail(%v) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

func TestNewOmitsPhoneScope(t *testing.T) {
	client := New(&Config{ClientID: "id", ClientSecret: "secret"})
	want := []string{"openid", "profile", "email"}
	if len(client.Scopes) != len(want) {
		t.Fatalf("Scopes = %v, want %v", client.Scopes, want)
	}
	for i, s := range want {
		if client.Scopes[i] != s {
			t.Fatalf("Scopes[%d] = %q, want %q", i, client.Scopes[i], s)
		}
	}
}
