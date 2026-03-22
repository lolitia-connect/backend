package captcha

import (
	"context"

	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"

	"github.com/perfect-panel/server/pkg/xerr"
)

// VerifyInput holds the captcha fields from a login/register/reset request.
type VerifyInput struct {
	CaptchaId   string
	CaptchaCode string
	CfToken     string
	SliderToken string
	IP          string
}

// VerifyCaptcha validates the captcha according to captchaType.
// Returns nil when captchaType is empty / unrecognised (i.e. captcha disabled).
func VerifyCaptcha(
	ctx context.Context,
	redisClient *redis.Client,
	captchaType string,
	turnstileSecret string,
	input VerifyInput,
) error {
	switch captchaType {
	case string(CaptchaTypeLocal):
		if input.CaptchaId == "" || input.CaptchaCode == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "captcha required")
		}
		svc := NewService(Config{
			Type:        CaptchaTypeLocal,
			RedisClient: redisClient,
		})
		valid, err := svc.Verify(ctx, input.CaptchaId, input.CaptchaCode, input.IP)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify captcha error")
		}
		if !valid {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "invalid captcha")
		}

	case string(CaptchaTypeTurnstile):
		if input.CfToken == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "captcha required")
		}
		svc := NewService(Config{
			Type:            CaptchaTypeTurnstile,
			TurnstileSecret: turnstileSecret,
		})
		valid, err := svc.Verify(ctx, input.CfToken, "", input.IP)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify captcha error")
		}
		if !valid {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "invalid captcha")
		}

	case string(CaptchaTypeSlider):
		if input.SliderToken == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "slider captcha required")
		}
		sliderSvc := NewSliderService(redisClient)
		valid, err := sliderSvc.VerifySliderToken(ctx, input.SliderToken)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify captcha error")
		}
		if !valid {
			return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "invalid slider captcha")
		}
	}

	return nil
}
