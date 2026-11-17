package alipay

import (
	"context"
	"net/url"
	"time"

	paymentutil "github.com/perfect-panel/server/pkg/payment"
	"github.com/pkg/errors"
	"github.com/smartwalle/alipay/v3"
	"go.uber.org/zap"
)

type Config struct {
	AppId       string
	PrivateKey  string
	PublicKey   string
	InvoiceName string
	NotifyURL   string
	Sandbox     bool
}

type Notification struct {
	OrderNo string
	TradeNo string
	Amount  int64
	Status  Status
}

type Status string

const (
	Success  Status = "TRADE_SUCCESS"
	Pending  Status = "WAIT_BUYER_PAY"
	Closed   Status = "TRADE_CLOSED"
	Finished Status = "TRADE_FINISHED"
	Error    Status = "TRADE_ERROR"
)

type Client struct {
	Config
	client *alipay.Client
}
type Order struct {
	OrderNo   string
	Amount    int64
	ExpiresAt time.Time
}

func NewClient(c Config) *Client {
	client, err := alipay.New(c.AppId, c.PrivateKey, !c.Sandbox)
	if err != nil {
		zap.S().Error("[Alipay] NewClient failed: ", zap.Any("errors", err), zap.Any("config", c))
		return nil
	}
	err = client.LoadAliPayPublicKey(c.PublicKey)
	if err != nil {
		zap.S().Error("[Alipay] NewClient failed: ", zap.Any("errors", err), zap.Any("config", c))
	}
	return &Client{
		Config: c,
		client: client,
	}
}

func (c *Client) PreCreateTrade(ctx context.Context, order Order) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("invalid Alipay configuration")
	}
	expiry := ""
	if !order.ExpiresAt.IsZero() {
		if !order.ExpiresAt.After(time.Now()) {
			return "", errors.New("order payment window expired")
		}
		expiry = order.ExpiresAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	trade, err := c.client.TradePreCreate(ctx, alipay.TradePreCreate{
		Trade: alipay.Trade{
			OutTradeNo:  order.OrderNo,
			TotalAmount: paymentutil.FormatAmount(order.Amount),
			TimeExpire:  expiry,
			Subject:     c.InvoiceName,
			NotifyURL:   c.NotifyURL,
		},
	})
	if err != nil {
		return "", err
	}
	if trade.Code != alipay.CodeSuccess {
		return "", errors.New("PreCreateTrade failed: " + trade.Msg)
	}
	return trade.QRCode, nil
}

func (c *Client) QueryTrade(ctx context.Context, orderNo string) (Status, error) {
	receipt, err := c.QueryPayment(ctx, orderNo)
	if err != nil {
		return Error, err
	}
	return receipt.Status, nil
}

func (c *Client) QueryPayment(ctx context.Context, orderNo string) (*Notification, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("invalid Alipay configuration")
	}
	trade, err := c.client.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: orderNo})
	if err != nil {
		return nil, err
	}
	if trade == nil {
		return nil, errors.New("empty Alipay query response")
	}
	if trade.Code != alipay.CodeSuccess {
		if trade.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return &Notification{OrderNo: orderNo, Status: Closed}, nil
		}
		return nil, errors.New("Alipay trade lookup failed: " + trade.SubCode)
	}
	if trade.OutTradeNo != orderNo {
		return nil, errors.New("Alipay payment belongs to another order")
	}
	status := Status(trade.TradeStatus)
	if status != Success && status != Finished && status != Pending && status != Closed {
		return nil, errors.New("unknown Alipay payment state")
	}
	amount, err := paymentutil.ParseAmount(trade.TotalAmount)
	if err != nil {
		return nil, err
	}
	return &Notification{OrderNo: trade.OutTradeNo, TradeNo: trade.TradeNo, Amount: amount, Status: status}, nil
}

func (c *Client) CloseTrade(ctx context.Context, orderNo string) error {
	if c == nil || c.client == nil {
		return errors.New("invalid Alipay configuration")
	}
	response, err := c.client.TradeClose(ctx, alipay.TradeClose{OutTradeNo: orderNo})
	if err != nil {
		return err
	}
	if response.Code != alipay.CodeSuccess && response.SubCode != "ACQ.TRADE_NOT_EXIST" && response.SubCode != "ACQ.TRADE_HAS_CLOSE" {
		return errors.New("Alipay trade cannot be closed: " + response.SubCode)
	}
	return nil
}

func (c *Client) DecodeNotification(ctx context.Context, form url.Values) (*Notification, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("invalid Alipay configuration")
	}
	notify, err := c.client.DecodeNotification(ctx, form)
	if err != nil {
		return nil, err
	}

	amount, err := paymentutil.ParseAmount(notify.TotalAmount)
	if err != nil {
		return nil, err
	}
	return &Notification{
		OrderNo: notify.OutTradeNo,
		TradeNo: notify.TradeNo,
		Amount:  amount,
		Status:  Status(notify.TradeStatus),
	}, nil
}
