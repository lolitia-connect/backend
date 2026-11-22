package oauthstate

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type rejectedState string

func (r rejectedState) Error() string { return string(r) }

func IsRejected(err error) bool {
	var rejection rejectedState
	return errors.Is(err, redis.Nil) || errors.As(err, &rejection)
}

type State struct {
	Purpose  string `json:"purpose"`
	UserID   int64  `json:"user_id"`
	Redirect string `json:"redirect"`
}

func ValidateRedirect(redirect, siteHost string) error {
	target, err := url.Parse(redirect)
	if err != nil || target.User != nil || target.Host == "" || (target.Scheme != "https" && target.Scheme != "http") || target.Fragment != "" || strings.ContainsAny(redirect, "\r\n\\") {
		return errors.New("invalid OAuth redirect")
	}
	if siteHost == "" {
		return errors.New("site host is required for OAuth redirects")
	}
	if !strings.Contains(siteHost, "://") {
		siteHost = "https://" + siteHost
	}
	pin, err := url.Parse(siteHost)
	if err != nil || pin.User != nil || pin.Host == "" || !strings.EqualFold(target.Host, pin.Host) || !strings.EqualFold(target.Scheme, pin.Scheme) {
		return errors.New("OAuth redirect does not match the configured site")
	}
	return nil
}

func Issue(ctx context.Context, client *redis.Client, provider, purpose string, userID int64, redirect string) (string, error) {
	if client == nil || (purpose != "login" && purpose != "bind") || (purpose == "bind" && userID <= 0) {
		return "", errors.New("invalid OAuth state scope")
	}
	code := strings.ReplaceAll(uuid.NewString(), "-", "")
	payload, err := json.Marshal(State{Purpose: purpose, UserID: userID, Redirect: redirect})
	if err != nil {
		return "", err
	}
	return code, client.Set(ctx, provider+":"+code, string(payload), 5*time.Minute).Err()
}

func Read(ctx context.Context, client *redis.Client, provider, code string) (*State, error) {
	if len(code) != 32 {
		return nil, rejectedState("invalid OAuth state")
	}
	if client == nil {
		return nil, errors.New("OAuth state store unavailable")
	}
	payload, err := client.Get(ctx, provider+":"+code).Result()
	if err != nil {
		return nil, err
	}
	var state State
	if json.Unmarshal([]byte(payload), &state) != nil || (state.Purpose != "login" && state.Purpose != "bind") || state.Redirect == "" {
		return nil, errors.New("invalid OAuth state")
	}
	return &state, nil
}

func Consume(ctx context.Context, client *redis.Client, provider, purpose string, userID int64, code string) (string, error) {
	state, err := Read(ctx, client, provider, code)
	if err != nil {
		return "", err
	}
	if state.Purpose != purpose || state.UserID != userID {
		return "", rejectedState("OAuth state belongs to another flow or account")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	removed, err := client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end; return 0`, []string{provider + ":" + code}, string(payload)).Int64()
	if err != nil {
		return "", err
	}
	if removed != 1 {
		return "", rejectedState("OAuth state was already consumed")
	}
	return state.Redirect, nil
}
