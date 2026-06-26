package stripe

import (
	"testing"

	"github.com/stripe/stripe-go/v81"
)

func TestStripeAlipay(t *testing.T) {
	t.Skipf("Skip TestStripeAlipay test")
	client := NewClient(Config{
		WebhookSecret: "",
	})
	order := Order{
		OrderNo:   "JS20210719123456789",
		Subscribe: "测试",
		Amount:    100,
		Currency:  string(stripe.CurrencyGBP),
		Payment:   "alipay",
	}
	user := User{
		UserId: 1,
		Email:  "tension@ppanel.dev",
	}
	result, err := client.CreateCheckoutSession(&order, &user, "https://example.com/success", "https://example.com/cancel")
	if err != nil {
		t.Error(err.Error())
	}
	t.Logf("CheckoutURL: %s\n", result.CheckoutURL)
}

func TestStripeWechat(t *testing.T) {
	t.Skipf("Skip TestStripeWechat test")
	client := NewClient(Config{
		SecretKey:     "SecretKey",
		PublicKey:     "PublicKey",
		WebhookSecret: "",
	})
	order := Order{
		OrderNo:   "JS20210719123456789",
		Subscribe: "测试",
		Amount:    100,
		Currency:  string(stripe.CurrencyGBP),
		Payment:   "wechat_pay",
	}
	user := User{
		UserId: 1,
		Email:  "tension@ppanel.dev",
	}
	result, err := client.CreateCheckoutSession(&order, &user, "https://example.com/success", "https://example.com/cancel")
	if err != nil {
		t.Error(err.Error())
	}
	t.Logf("CheckoutURL: %s\n", result.CheckoutURL)
}
