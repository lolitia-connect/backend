package telegram

import (
	"encoding/base64"
	"net/http"
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
	data := &AuthData{
		Id:        &id,
		FirstName: &firstName,
		LastName:  &lastName,
		Username:  &username,
		PhotoUrl:  &photoURL,
		AuthDate:  &authDate,
	}
	token := "7651491571:AAEVQma6niHhtqEYDowAEpPo6Fq69BWvRU8"
	hash := computeHash(data, []byte(token))
	text := base64.StdEncoding.EncodeToString([]byte(`{"id":824626803,"first_name":"Chang lue","last_name":"Tsen","username":"tension_c","photo_url":"https://t.me/i/userpic/320/aMK6HDsJjseubWQbkv4iX8vBEAz7HVSx7vAnD0KgKEU.jpg","auth_date":1737819074,"hash":"` + hash + `"}`))

	parsed, err := ParseAndValidateBase64([]byte(text), token)
	if err != nil {
		t.Error(err)
	}
	if parsed == nil || parsed.Id == nil || *parsed.Id != id {
		t.Fatalf("unexpected parsed data: %#v", parsed)
	}

}
