package captcha

import (
	"context"
	"fmt"
	"time"

	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
)

type localService struct {
	redis  *redis.Client
	driver base64Captcha.Driver
}

func newLocalService(redisClient *redis.Client) Service {
	// Configure captcha driver
	driver := base64Captcha.NewDriverDigit(80, 240, 5, 0.7, 80)
	return &localService{
		redis:  redisClient,
		driver: driver,
	}
}

func (s *localService) Generate(ctx context.Context) (id string, image string, err error) {
	// Generate captcha
	captcha := base64Captcha.NewCaptcha(s.driver, &redisStore{
		redis: s.redis,
		ctx:   ctx,
	})

	id, b64s, answer, err := captcha.Generate()
	if err != nil {
		return "", "", err
	}

	// Store answer in Redis with 5 minute expiration
	key := fmt.Sprintf("captcha:%s", id)
	err = s.redis.Set(ctx, key, answer, 5*time.Minute).Err()
	if err != nil {
		return "", "", err
	}

	return id, b64s, nil
}

func (s *localService) Verify(ctx context.Context, id string, code string, ip string) (bool, error) {
	if id == "" || code == "" {
		return false, nil
	}

	key := fmt.Sprintf("captcha:%s", id)

	// Get answer from Redis
	answer, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// Delete captcha after verification (one-time use)
	s.redis.Del(ctx, key)

	// Verify code
	return answer == code, nil
}

func (s *localService) GetType() CaptchaType {
	return CaptchaTypeLocal
}

// redisStore implements base64Captcha.Store interface
type redisStore struct {
	redis *redis.Client
	ctx   context.Context
}

func (r *redisStore) Set(id string, value string) error {
	key := fmt.Sprintf("captcha:%s", id)
	return r.redis.Set(r.ctx, key, value, 5*time.Minute).Err()
}

func (r *redisStore) Get(id string, clear bool) string {
	key := fmt.Sprintf("captcha:%s", id)
	val, err := r.redis.Get(r.ctx, key).Result()
	if err != nil {
		return ""
	}
	if clear {
		r.redis.Del(r.ctx, key)
	}
	return val
}

func (r *redisStore) Verify(id, answer string, clear bool) bool {
	v := r.Get(id, clear)
	return v == answer
}
