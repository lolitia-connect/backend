package stripe

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhookendpoint"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/webhook"
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
}

type CheckoutResult struct {
	PublishableKey string
	TradeNo        string
	CheckoutURL    string
}

func NewClient(config Config) *Client {
	return &Client{
		Config: config,
	}
}

// SearchStripeCustomer  Search for a Stripe customer by email or user ID
func (c *Client) SearchStripeCustomer(user *User) (*stripe.Customer, error) {
	stripe.Key = c.SecretKey
	params := &stripe.CustomerSearchParams{}
	if user.Email != "" {
		params.SearchParams.Query = fmt.Sprintf("email:'%s'", user.Email)
	} else {
		params.SearchParams.Query = fmt.Sprintf("metadata['user_id']:'%d'", user.UserId)
	}
	result := customer.Search(params)
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
	stripe.Key = c.SecretKey
	customerData := &stripe.CustomerParams{}
	if user.Email != "" {
		customerData.Email = &user.Email
	}
	customerData.AddMetadata("user_id", strconv.FormatInt(user.UserId, 10))
	return customer.New(customerData)
}

// QueryOrderStatus Query the status of the order via Checkout Session
func (c *Client) QueryOrderStatus(orderNo string) (bool, error) {
	stripe.Key = c.SecretKey
	sess, err := session.Get(orderNo, nil)
	if err != nil {
		return false, err
	}
	return sess.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid, nil
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
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			logger.Error("Failed to unmarshal checkout session", logger.Field("error", err.Error()))
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
			Amount:    sess.AmountTotal,
		}, nil

	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
			logger.Error("Failed to unmarshal payment intent", logger.Field("error", err.Error()))
			return nil, err
		}
		orderNo := paymentIntent.Metadata["order_no"]
		userId := paymentIntent.Metadata["user_id"]
		var method string
		if paymentIntent.PaymentMethod != nil && paymentIntent.PaymentMethod.ID != "" {
			result, err := c.RetrievePaymentMethod(paymentIntent.PaymentMethod.ID)
			if err != nil {
				logger.Error("[stripe] Payment callback query payment method error", logger.Field("errors", err.Error()))
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
			Amount:    paymentIntent.Amount,
		}, nil

	case "payment_intent.payment_failed":
		logger.Info("[Stripe] Payment failed", logger.Field("event_type", event.Type))
		return &NotifyResult{
			EventType: string(event.Type),
		}, nil

	default:
		logger.Info("[Stripe] Ignoring unhandled event type", logger.Field("event_type", event.Type))
		return &NotifyResult{
			EventType: string(event.Type),
		}, nil
	}
}

// RetrievePaymentMethod 查询支付方式
func (c *Client) RetrievePaymentMethod(id string) (*stripe.PaymentMethod, error) {
	stripe.Key = c.SecretKey
	return paymentmethod.Get(id, nil)
}

// CreateCheckoutSession creates a Stripe Checkout Session for redirect-based payment
func (c *Client) CreateCheckoutSession(order *Order, user *User, successURL, cancelURL string) (*CheckoutResult, error) {
	stripe.Key = c.SecretKey

	// Pass the configured payment method directly to Stripe Checkout Session
	// Supported values include: card, alipay, wechat_pay, google_pay, apple_pay,
	// paypal, klarna, afterpay_clearpay, us_bank_account, etc.
	// Google Pay and Apple Pay are also automatically available when 'card' is included.
	paymentTypes := []*string{stripe.String(order.Payment)}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: paymentTypes,
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(order.Currency),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("Order " + order.OrderNo),
					},
					UnitAmount: stripe.Int64(order.Amount),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		ClientReferenceID: stripe.String(order.OrderNo),
		Metadata: map[string]string{
			"order_no":  order.OrderNo,
			"user_id":   strconv.FormatInt(user.UserId, 10),
			"subscribe": order.Subscribe,
		},
	}

	if order.StatementDescriptorSuffix != "" {
		// Statement descriptor suffix only applies to card payments
		if order.Payment == "card" || order.Payment == "google_pay" {
			// Stripe checkout session doesn't support StatementDescriptorSuffix directly
			// but we can set it via PaymentIntentData
			params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
				StatementDescriptorSuffix: stripe.String(order.StatementDescriptorSuffix),
				Description:               stripe.String(order.StatementDescriptorSuffix),
			}
		}
	}

	result, err := session.New(params)
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
	stripe.Key = c.SecretKey
	params := &stripe.WebhookEndpointParams{
		URL: stripe.String(url),
		EnabledEvents: []*string{
			stripe.String("checkout.session.completed"),
			stripe.String("payment_intent.succeeded"),
			stripe.String("payment_intent.payment_failed"),
		},
	}
	return webhookendpoint.New(params)
}
