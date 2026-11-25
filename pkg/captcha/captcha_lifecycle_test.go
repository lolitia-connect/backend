package captcha

import (
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestSliderShapesAndTrailRulesRejectRoboticInput(t *testing.T) {
	for _, shape := range []sliderShape{shapeSquare, shapeCircle, shapeDiamond, shapeStar, shapeTriangle, shapeTrapezoid} {
		if !inMask(sliderBlockSize/2, sliderBlockSize/2, shape) {
			t.Fatalf("shape %d does not contain its center", shape)
		}
	}
	base := []TrailPoint{{X: 0, Y: 0, T: 0}, {X: 10, Y: 1, T: 50}, {X: 20, Y: 3, T: 120}, {X: 30, Y: 1, T: 210}, {X: 45, Y: 4, T: 320}, {X: 60, Y: 2, T: 450}, {X: 75, Y: 0, T: 590}, {X: 90, Y: 3, T: 740}, {X: 100, Y: 1, T: 900}}
	if !validateTrail(base, 100) {
		t.Fatal("human-like slider trail was rejected")
	}
	for _, test := range []struct {
		name  string
		trail []TrailPoint
		x     int
		want  bool
	}{
		{"too short", base[:7], 100, false},
		{"too fast", []TrailPoint{{X: 0, T: 0}, {X: 10, T: 1}, {X: 20, T: 2}, {X: 30, T: 3}, {X: 40, T: 4}, {X: 50, T: 5}, {X: 60, T: 6}, {X: 70, T: 7}}, 70, false},
		{"starts too far right", append([]TrailPoint{{X: 11, Y: 0, T: 0}}, base[1:]...), 100, false},
		{"large jump", append([]TrailPoint{}, base[:4]...), 100, false},
		{"flat path", []TrailPoint{{X: 0, Y: 0, T: 0}, {X: 10, Y: 0, T: 50}, {X: 20, Y: 0, T: 120}, {X: 30, Y: 0, T: 210}, {X: 45, Y: 0, T: 320}, {X: 60, Y: 0, T: 450}, {X: 75, Y: 0, T: 590}, {X: 90, Y: 0, T: 740}, {X: 100, Y: 0, T: 900}}, 100, false},
		{"wrong final position", base, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			trail := test.trail
			if test.name == "large jump" {
				trail = append(trail, TrailPoint{X: 200, Y: 1, T: 900}, TrailPoint{X: 210, Y: 2, T: 1000}, TrailPoint{X: 220, Y: 3, T: 1100}, TrailPoint{X: 230, Y: 2, T: 1200}, TrailPoint{X: 240, Y: 3, T: 1300})
			}
			if got := validateTrail(trail, test.x); got != test.want {
				t.Fatalf("validateTrail() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLocalCaptchaGeneratesAndConsumesVerification(t *testing.T) {
	ctx := t.Context()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	service := NewService(Config{Type: CaptchaTypeLocal, RedisClient: client})
	id, image, err := service.Generate(ctx)
	if err != nil || id == "" || image == "" || service.GetType() != CaptchaTypeLocal {
		t.Fatalf("local captcha = id %q, image %t, type %q, err %v", id, image != "", service.GetType(), err)
	}
	answer, err := client.Get(ctx, "captcha:"+id).Result()
	if err != nil || answer == "" {
		t.Fatalf("generated challenge was not stored: %v", err)
	}
	if err := VerifyCaptcha(ctx, client, string(CaptchaTypeLocal), "", VerifyInput{CaptchaId: id, CaptchaCode: answer}); err != nil {
		t.Fatalf("correct local captcha rejected: %v", err)
	}
	if err := VerifyCaptcha(ctx, client, string(CaptchaTypeLocal), "", VerifyInput{CaptchaId: id, CaptchaCode: answer}); err == nil {
		t.Fatal("local captcha remained reusable")
	}
	otherID, _, err := service.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := service.Verify(ctx, otherID, "wrong-answer", "192.0.2.1"); err != nil || ok {
		t.Fatalf("wrong local captcha accepted: ok=%v, err=%v", ok, err)
	}
	if err := VerifyCaptcha(ctx, client, string(CaptchaTypeLocal), "", VerifyInput{}); err == nil {
		t.Fatal("missing local captcha fields were accepted")
	}
	if err := VerifyCaptcha(ctx, client, "", "", VerifyInput{}); err != nil {
		t.Fatalf("disabled captcha rejected request: %v", err)
	}
}

func TestSliderCaptchaValidatesShapeTrailAndSingleUseToken(t *testing.T) {
	ctx := t.Context()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	service := NewSliderService(client)
	id, background, block, err := service.GenerateSlider(ctx)
	if err != nil || id == "" || background == "" || block == "" || service.GetType() != CaptchaTypeSlider {
		t.Fatalf("slider generation failed: id=%q background=%t block=%t err=%v", id, background != "", block != "", err)
	}
	if token, err := service.VerifySlider(ctx, id, 0, 0, "not-json"); err == nil || token != "" {
		t.Fatal("invalid slider trail was accepted")
	}
	stored := client.Get(ctx, "captcha:slider:"+id).Val()
	var position sliderData
	if err := json.Unmarshal([]byte(stored), &position); err != nil {
		t.Fatal(err)
	}
	trail := make([]TrailPoint, 0, 10)
	times := []int64{0, 30, 70, 120, 180, 250, 330, 420, 520, 650}
	yValues := []int{0, 1, 3, 1, 4, 2, 0, 3, 1, 2}
	for i := 0; i < len(times); i++ {
		trail = append(trail, TrailPoint{X: i * position.X / 9, Y: yValues[i], T: times[i]})
	}
	encoded, err := json.Marshal(trail)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := service.VerifySlider(ctx, id, position.X, position.Y, string(encoded)); err != nil || token == "" {
		t.Fatalf("valid slider trail rejected: token=%q err=%v", token, err)
	} else if valid, err := service.VerifySliderToken(ctx, token); err != nil || !valid {
		t.Fatalf("slider token rejected: valid=%v err=%v", valid, err)
	} else if valid, err := service.VerifySliderToken(ctx, token); err != nil || valid {
		t.Fatalf("slider token was reusable: valid=%v err=%v", valid, err)
	}
	if err := VerifyCaptcha(ctx, client, string(CaptchaTypeSlider), "", VerifyInput{}); err == nil {
		t.Fatal("missing slider token was accepted")
	}
}
