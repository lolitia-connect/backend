package google

import (
	"context"
	"encoding/json"
	"io"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}
type Client struct {
	*oauth2.Config
}
type UserInfo struct {
	OpenID        string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"verified_email"`
}

func New(config *Config) *Client {
	return &Client{
		&oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{"openid", "profile", "email"},
			Endpoint:     google.Endpoint,
		},
	}
}

// GetUserInfo fetches the Google profile. The response is decoded into a typed
// struct with a tolerant verified_email field because Google has been observed
// returning it as either a boolean or a string; asserting on a
// map[string]interface{} would panic the request handler.
func (c *Client) GetUserInfo(token string) (*UserInfo, error) {
	client := c.Config.Client(context.Background(), &oauth2.Token{AccessToken: token})
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		zap.S().Error("[Google OAuth 2.0] Get User Info", zap.Any("error", err.Error()))
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		zap.S().Error("[Google OAuth 2.0] Read response body", zap.Any("error", err.Error()))
		return nil, err
	}

	var raw struct {
		ID            string      `json:"id"`
		Email         string      `json:"email"`
		Name          string      `json:"name"`
		Picture       string      `json:"picture"`
		VerifiedEmail interface{} `json:"verified_email"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		zap.S().Error("[Google OAuth 2.0] Decode User Info", zap.Any("error", err.Error()))
		return nil, err
	}

	return &UserInfo{
		OpenID:        raw.ID,
		Email:         raw.Email,
		Name:          raw.Name,
		Picture:       raw.Picture,
		VerifiedEmail: parseVerifiedEmail(raw.VerifiedEmail),
	}, nil
}

// parseVerifiedEmail normalizes the verified_email field, which Google may
// send as a bool or as a string.
func parseVerifiedEmail(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}
