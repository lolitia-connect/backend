package telegram

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/perfect-panel/server/pkg/hertzx"
)

func TestOAuth(t *testing.T) {
	t.Skipf("Skip TestOAuth test")
	router := hertzx.Default()
	router.LoadHTMLGlob("./*")
	router.GET("/telegram", func(c *hertzx.Context) {
		c.HTML(http.StatusOK, "telegram.html", hertzx.H{
			"title":   "Hertz HTML Example",
			"message": "Hello, Hertz!",
		})
	})
	router.GET("/auth/telegram/callback", func(c *hertzx.Context) {

	})
	_ = router.RunTLS(":443", "server.crt", "server.key")
}

func TestBase64(t *testing.T) {
	id := int64(824626803)
	firstName := "Chang lue"
	lastName := "Tsen"
	username := "tension_c"
	photoURL := "https://t.me/i/userpic/320/aMK6HDsJjseubWQbkv4iX8vBEAz7HVSx7vAnD0KgKEU.jpg"
	authDate := int64(1737819074)
	payload := fmt.Sprintf(`{"id":%d,"first_name":%q,"last_name":%q,"username":%q,"photo_url":%q,"auth_date":%d}`,
		id, firstName, lastName, username, photoURL, authDate)
	token := "7651491571:AAEVQma6niHhtqEYDowAEpPo6Fq69BWvRU8"

	parsed, err := ParseAuthDataJson([]byte(payload))
	if err != nil {
		t.Fatalf("ParseAuthDataJson error: %v", err)
	}
	hash := computeHash(parsed.raw, []byte(token))
	signed := strings.TrimSuffix(payload, "}") + `,"hash":"` + hash + `"}`
	text := base64.StdEncoding.EncodeToString([]byte(signed))

	validated, err := ParseAndValidateBase64([]byte(text), token)
	if err != nil {
		t.Fatalf("ParseAndValidateBase64 error: %v", err)
	}
	if validated == nil || validated.Id == nil || *validated.Id != id {
		t.Fatalf("unexpected parsed data: %#v", validated)
	}
}
