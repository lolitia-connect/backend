package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhookendpoint"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/paymentintent"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/webhook"
	"go.uber.org/zap"
)

type Config struct {
	PublicKey     string
	SecretKey     string
	WebhookSecret string
}

type User struct {
	UserId int64
	Email  string
}
type NotifyResult struct {
	EventType string
	OrderNo   string
	TradeNo   string
	Method    string
	UserId    int64
	Amount    int64
	Currency  string
	Paid      bool
}
type Order struct {
	OrderNo                   string
	Subscribe                 string
	Amount                    int64
	Currency                  string
	Payment                   string
	StatementDescriptorSuffix string
}

type Client struct {
	Config
	backend stripe.Backend
}

type CheckoutResult struct {
	PublishableKey string
	TradeNo        string
	CheckoutURL    string
}

func NewClient(config Config) *Client {
	return &Client{
		Config:  config,
		backend: stripe.GetBackend(stripe.APIBackend),
	}
}

// SearchStripeCustomer  Search for a Stripe customer by email or user ID
func (c *Client) SearchStripeCustomer(user *User) (*stripe.Customer, error) {
	params := &stripe.CustomerSearchParams{}
	if user.Email != "" {
		params.SearchParams.Query = fmt.Sprintf("email:'%s'", user.Email)
	} else {
		params.SearchParams.Query = fmt.Sprintf("metadata['user_id']:'%d'", user.UserId)
	}
	result := (customer.Client{B: c.backend, Key: c.SecretKey}).Search(params)
	if result.Err() != nil {
		fmt.Printf("Error: %v\n", result.Err().Error())
		return nil, result.Err()
	}

	if len(result.CustomerSearchResult().Data) != 0 {
		return result.CustomerSearchResult().Data[0], nil
	}
	return nil, nil
}

// CreateCustomer Create a new Stripe customer
func (c *Client) CreateCustomer(user *User) (*stripe.Customer, error) {
	customerData := &stripe.CustomerParams{}
	if user.Email != "" {
		customerData.Email = &user.Email
	}
	customerData.AddMetadata("user_id", strconv.FormatInt(user.UserId, 10))
	return (customer.Client{B: c.backend, Key: c.SecretKey}).New(customerData)
}

// QueryOrderStatus Query the status of the order via Checkout Session
func (c *Client) QueryOrderStatus(orderNo string) (bool, error) {
	receipt, err := c.QueryPayment(context.Background(), orderNo)
	if err != nil {
		return false, err
	}
	return receipt.Paid, nil
}

func (c *Client) QueryPayment(ctx context.Context, tradeNo string) (*NotifyResult, error) {
	if strings.HasPrefix(tradeNo, "pi_") {
		params := &stripe.PaymentIntentParams{}
		params.Context = ctx
		intent, err := (paymentintent.Client{B: c.backend, Key: c.SecretKey}).Get(tradeNo, params)
		if err != nil {
			return nil, err
		}
		return &NotifyResult{OrderNo: intent.Metadata["order_no"], TradeNo: intent.ID, Amount: FromStripeAmount(intent.AmountReceived, string(intent.Currency)), Currency: string(intent.Currency), Paid: intent.Status == stripe.PaymentIntentStatusSucceeded}, nil
	}
	if !strings.HasPrefix(tradeNo, "cs_") {
		return nil, fmt.Errorf("invalid Stripe payment reference")
	}
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	current, err := (session.Client{B: c.backend, Key: c.SecretKey}).Get(tradeNo, params)
	if err != nil {
		return nil, err
	}
	orderNo := current.ClientReferenceID
	if orderNo == "" {
		orderNo = current.Metadata["order_no"]
	}
	return &NotifyResult{OrderNo: orderNo, TradeNo: current.ID, Amount: FromStripeAmount(current.AmountTotal, string(current.Currency)), Currency: string(current.Currency), Paid: current.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid}, nil
}

// ParseNotify
func (c *Client) ParseNotify(payload []byte, signature string) (*NotifyResult, error) {
	event, err := webhook.ConstructEventWithOptions(payload, signature, c.Config.WebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		return nil, err
	}

	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			zap.S().Error("Failed to unmarshal checkout session", zap.Any("error", err.Error()))
			return nil, err
		}
		orderNo := sess.ClientReferenceID
		if orderNo == "" {
			orderNo = sess.Metadata["order_no"]
		}
		userId := sess.Metadata["user_id"]
		uid, _ := strconv.ParseInt(userId, 10, 64)
		var method string
		if sess.PaymentMethodTypes != nil {
			for _, t := range sess.PaymentMethodTypes {
				method = string(t)
				break
			}
		}
		return &NotifyResult{
			EventType: string(event.Type),
			OrderNo:   orderNo,
			TradeNo:   sess.ID,
			UserId:    uid,
			Method:    method,
			Amount:    FromStripeAmount(sess.AmountTotal, string(sess.Currency)),
			Currency:  string(sess.Currency),
			Paid:      sess.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid,
		}, nil

	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			zap.S().Error("Failed to unmarshal payment intent", zap.Any("error", err.Error()))
			return nil, err
		}
		orderNo := paymentIntent.Metadata["order_no"]
		userId := paymentIntent.Metadata["user_id"]
		var method string
		if paymentIntent.PaymentMethod != nil && paymentIntent.PaymentMethod.ID != "" {
			result, err := c.RetrievePaymentMethod(paymentIntent.PaymentMethod.ID)
			if err != nil {
				zap.S().Error("[stripe] Payment callback query payment method error", zap.Any("errors", err.Error()))
			}
			if result != nil {
				method = string(result.Type)
			}
		}
		uid, _ := strconv.ParseInt(userId, 10, 64)
		return &NotifyResult{
			EventType: string(event.Type),
			OrderNo:   orderNo,
			TradeNo:   paymentIntent.ID,
			UserId:    uid,
			Method:    method,
			Amount:    FromStripeAmount(paymentIntent.AmountReceived, string(paymentIntent.Currency)),
			Currency:  string(paymentIntent.Currency),
			Paid:      paymentIntent.Status == stripe.PaymentIntentStatusSucceeded,
		}, nil

	case "payment_intent.payment_failed":
		zap.S().Info("[Stripe] Payment failed", zap.Any("event_type", event.Type))
		return &NotifyResult{
			EventType: string(event.Type),
		}, nil

	default:
		zap.S().Info("[Stripe] Ignoring unhandled event type", zap.Any("event_type", event.Type))
		return &NotifyResult{
			EventType: string(event.Type),
		}, nil
	}
}

// RetrievePaymentMethod 查询支付方式
func (c *Client) RetrievePaymentMethod(id string) (*stripe.PaymentMethod, error) {
	return (paymentmethod.Client{B: c.backend, Key: c.SecretKey}).Get(id, nil)
}

// CreateCheckoutSession creates a Stripe Checkout Session for redirect-based payment
func (c *Client) CreateCheckoutSession(order *Order, user *User, successURL, cancelURL string) (*CheckoutResult, error) {
	return c.CreateCheckoutSessionContext(context.Background(), order, user, successURL, cancelURL)
}

func (c *Client) CreateCheckoutSessionContext(ctx context.Context, order *Order, user *User, successURL, cancelURL string) (*CheckoutResult, error) {
	if order.Amount <= 0 || order.Amount > math.MaxInt64/10 {
		return nil, fmt.Errorf("invalid Stripe amount")
	}

	// Pass the configured payment method directly to Stripe Checkout Session
	// Supported values include: card, alipay, wechat_pay, google_pay, apple_pay,
	// paypal, klarna, afterpay_clearpay, us_bank_account, etc.
	// Google Pay and Apple Pay are also automatically available when 'card' is included.
	paymentTypes := []*string{stripe.String(order.Payment)}

	metadata := map[string]string{
		"order_no":  order.OrderNo,
		"user_id":   strconv.FormatInt(user.UserId, 10),
		"subscribe": order.Subscribe,
	}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: paymentTypes,
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(order.Currency),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("Order " + order.OrderNo),
					},
					UnitAmount: stripe.Int64(ToStripeAmount(RoundHundredths(order.Amount, order.Currency), order.Currency)),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		ClientReferenceID: stripe.String(order.OrderNo),
		Metadata:          metadata,
	}

	// Always set PaymentIntentData to ensure metadata is copied to the PaymentIntent.
	// Some Stripe API versions do not automatically copy Checkout Session metadata to PaymentIntent.
	intentData := &stripe.CheckoutSessionPaymentIntentDataParams{
		Metadata: metadata,
	}

	if order.StatementDescriptorSuffix != "" {
		// Statement descriptor suffix only applies to card payments
		if order.Payment == "card" || order.Payment == "google_pay" {
			intentData.StatementDescriptorSuffix = stripe.String(order.StatementDescriptorSuffix)
			intentData.Description = stripe.String(order.StatementDescriptorSuffix)
		}
	}

	params.PaymentIntentData = intentData
	params.Context = ctx
	params.SetIdempotencyKey("checkout:" + order.OrderNo)

	result, err := (session.Client{B: c.backend, Key: c.SecretKey}).New(params)
	if err != nil {
		return nil, err
	}

	return &CheckoutResult{
		PublishableKey: c.PublicKey,
		TradeNo:        result.ID,
		CheckoutURL:    result.URL,
	}, nil
}

// CreateWebhookEndpoint 创建 webhook endpoint
func (c *Client) CreateWebhookEndpoint(url string) (*stripe.WebhookEndpoint, error) {
	params := &stripe.WebhookEndpointParams{
		URL: stripe.String(url),
		EnabledEvents: []*string{
			stripe.String("checkout.session.completed"),
			stripe.String("checkout.session.async_payment_succeeded"),
			stripe.String("checkout.session.async_payment_failed"),
			stripe.String("payment_intent.succeeded"),
			stripe.String("payment_intent.payment_failed"),
		},
	}
	return (webhookendpoint.Client{B: c.backend, Key: c.SecretKey}).New(params)
}

func (c *Client) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	params := &stripe.WebhookEndpointParams{}
	params.Context = ctx
	_, err := (webhookendpoint.Client{B: c.backend, Key: c.SecretKey}).Del(id, params)
	return err
}

func (c *Client) CancelCheckout(ctx context.Context, tradeNo string) error {
	if strings.HasPrefix(tradeNo, "pi_") {
		client := paymentintent.Client{B: c.backend, Key: c.SecretKey}
		params := &stripe.PaymentIntentParams{}
		params.Context = ctx
		intent, err := client.Get(tradeNo, params)
		if err != nil {
			return err
		}
		if intent.Status == stripe.PaymentIntentStatusCanceled {
			return nil
		}
		if intent.Status == stripe.PaymentIntentStatusSucceeded {
			return fmt.Errorf("Stripe payment already settled")
		}
		cancel := &stripe.PaymentIntentCancelParams{}
		cancel.Context = ctx
		_, err = client.Cancel(tradeNo, cancel)
		return err
	}
	client := session.Client{B: c.backend, Key: c.SecretKey}
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	current, err := client.Get(tradeNo, params)
	if err != nil {
		return err
	}
	if current.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
		return fmt.Errorf("Stripe payment already settled")
	}
	if current.Status == stripe.CheckoutSessionStatusExpired {
		return nil
	}
	if current.Status != stripe.CheckoutSessionStatusOpen {
		return fmt.Errorf("Stripe checkout cannot be canceled yet")
	}
	expire := &stripe.CheckoutSessionExpireParams{}
	expire.Context = ctx
	_, err = client.Expire(tradeNo, expire)
	return err
}
