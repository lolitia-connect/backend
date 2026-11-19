package usersession

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/redis/go-redis/v9"
)

var ErrRevoked = errors.New("user sessions revoked")

const EpochClaim = "SessionEpoch"

func Key(userID int64) string {
	return "auth:user_session_epoch:" + strconv.FormatInt(userID, 10)
}

func AcquireEpoch(ctx context.Context, client *redis.Client, userID int64) (string, error) {
	if client == nil || userID <= 0 {
		return "", errors.New("session store or user unavailable")
	}
	if err := client.SetNX(ctx, Key(userID), "i:"+uuid.NewString(), 0).Err(); err != nil {
		return "", err
	}
	return client.Get(ctx, Key(userID)).Result()
}

func Rotate(ctx context.Context, client *redis.Client, userID int64) (string, error) {
	epoch := "r:" + strconv.FormatInt(time.Now().UnixMilli(), 10) + ":" + uuid.NewString()
	if client == nil || userID <= 0 {
		return "", errors.New("session store or user unavailable")
	}
	return epoch, client.Set(ctx, Key(userID), epoch, 0).Err()
}

func Check(ctx context.Context, client *redis.Client, userID int64, claims map[string]any) error {
	current, err := client.Get(ctx, Key(userID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	value, exists := claims[EpochClaim]
	if !exists {
		if strings.HasPrefix(current, "r:") {
			return ErrRevoked
		}
		return nil
	}
	epoch, valid := value.(string)
	if !valid || epoch == "" || epoch != current {
		return ErrRevoked
	}
	return nil
}

func Issue(ctx context.Context, client *redis.Client, secret string, lifetime, userID int64, sessionID, loginType, expectedEpoch, identifier string) (string, error) {
	epoch, err := AcquireEpoch(ctx, client, userID)
	if err != nil {
		return "", err
	}
	if expectedEpoch != "" && epoch != expectedEpoch {
		return "", ErrRevoked
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	signed, err := jwt.NewJwtToken(secret, time.Now().Unix(), lifetime,
		jwt.WithOption("UserId", userID), jwt.WithOption("SessionId", sessionID),
		jwt.WithOption("LoginType", loginType), jwt.WithOption("identifier", identifier),
		jwt.WithOption(EpochClaim, epoch))
	if err != nil {
		return "", err
	}
	if err := client.Set(ctx, config.SessionIdKey+":"+sessionID, userID, time.Duration(lifetime)*time.Second).Err(); err != nil {
		return "", err
	}
	return signed, nil
}

func RevokedSince(ctx context.Context, client *redis.Client, userID int64, since time.Time) (bool, error) {
	epoch, err := client.Get(ctx, Key(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(epoch, "r:") {
		return false, nil
	}
	parts := strings.SplitN(epoch, ":", 3)
	if len(parts) != 3 {
		return true, nil
	}
	millis, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return true, nil
	}
	return millis >= since.UnixMilli(), nil
}

func End(ctx context.Context, client *redis.Client, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	return client.Del(ctx, config.SessionIdKey+":"+sessionID).Err()
}
