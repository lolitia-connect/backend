package alipayplus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	defaultAlipayClient "github.com/alipay/global-open-sdk-go/com/alipay/api"
	"github.com/alipay/global-open-sdk-go/com/alipay/api/model"
	requestPay "github.com/alipay/global-open-sdk-go/com/alipay/api/request/pay"
	alipayResponse "github.com/alipay/global-open-sdk-go/com/alipay/api/response"
	responsePay "github.com/alipay/global-open-sdk-go/com/alipay/api/response/pay"
	"github.com/alipay/global-open-sdk-go/com/alipay/api/tools"
	"github.com/pkg/errors"
)

type Config struct {
	ClientId        string
	MerchantId      string
	PrivateKey      string
	AlipayPublicKey string
	GatewayUrl      string
	Currency        string // USD, EUR 等
	PaymentMethod   string // ALIPAY_CN, ALIPAY_HK
	InvoiceName     string
	NotifyURL       string
	RedirectURL     string
}

type Notification struct {
	OrderNo string
	Amount  int64
	Status  Status
}

type Status string

const (
	Success Status = "SUCCESS"
	Pending Status = "PROCESSING"
	Closed  Status = "CANCELLED"
	Failed  Status = "FAIL"
	Error   Status = "ERROR"
)

type Order struct {
	OrderNo           string
	Amount            int64
	ReferenceBuyerId  string
	PaymentExpiryTime string
}

type Client struct {
	Config
	apiClient *defaultAlipayClient.DefaultAlipayClient
}

type payResultNotify struct {
	NotifyType       string                `json:"notifyType,omitempty"`
	Result           alipayResponse.Result `json:"result,omitempty"`
	PaymentRequestId string                `json:"paymentRequestId,omitempty"`
	PaymentAmount    *model.Amount         `json:"paymentAmount,omitempty"`
}

func NewClient(c Config) *Client {
	apiClient := defaultAlipayClient.NewDefaultAlipayClient(
		c.GatewayUrl,
		c.ClientId,
		c.PrivateKey,
		c.AlipayPublicKey,
	)

	return &Client{
		Config:    c,
		apiClient: apiClient,
	}
}

func (c *Client) PreCreateTrade(ctx context.Context, order Order) (string, error) {
	_ = ctx

	currency := strings.ToUpper(strings.TrimSpace(c.Currency))
	if currency == "" {
		return "", errors.New("currency is empty")
	}
	paymentMethod := strings.ToUpper(strings.TrimSpace(c.PaymentMethod))
	if paymentMethod == "" {
		return "", errors.New("paymentMethod is empty")
	}

	req, payReq := requestPay.NewAlipayPayRequest()
	amount := model.NewAmount(formatAmount(order.Amount), currency)
	payReq.PaymentRequestId = order.OrderNo
	payReq.ProductCode = model.ProductCodeType_CASHIER_PAYMENT
	payReq.PaymentAmount = amount
	payReq.PaymentMethod = &model.PaymentMethod{
		PaymentMethodType: paymentMethod,
	}
	payReq.SettlementStrategy = &model.SettlementStrategy{
		SettlementCurrency: currency,
	}
	payReq.PaymentNotifyUrl = c.NotifyURL
	payReq.PaymentRedirectUrl = c.RedirectURL
	if paymentExpiryTime := strings.TrimSpace(order.PaymentExpiryTime); paymentExpiryTime != "" {
		payReq.PaymentExpiryTime = paymentExpiryTime
	}

	payReq.Order = &model.Order{
		ReferenceOrderId: order.OrderNo,
		OrderDescription: c.InvoiceName,
		OrderAmount:      amount,
		Env: &model.Env{
			TerminalType: model.TerminalType_WEB,
		},
	}
	if referenceBuyerID := strings.TrimSpace(order.ReferenceBuyerId); referenceBuyerID != "" {
		payReq.Order.Buyer = &model.Buyer{ReferenceBuyerId: referenceBuyerID}
	}
	if strings.TrimSpace(c.MerchantId) != "" || strings.TrimSpace(c.InvoiceName) != "" {
		payReq.Order.Merchant = &model.Merchant{
			ReferenceMerchantId: c.MerchantId,
			MerchantName:        c.InvoiceName,
		}
	}

	result, err := c.apiClient.Execute(req)
	if err != nil {
		return "", err
	}

	resp, ok := result.(*responsePay.AlipayPayResponse)
	if !ok {
		return "", errors.New("unexpected pay response type")
	}

	if !isPayResponseSuccess(resp) {
		return "", errors.New("pay failed: " + responseMessage(resp))
	}

	if payload := resolvePaymentPayload(resp); payload != "" {
		return payload, nil
	}

	return "", errors.New("no payment payload found in alipay+ response")
}

func (c *Client) QueryTrade(ctx context.Context, orderNo string) (Status, error) {
	_ = ctx

	req, queryReq := requestPay.NewAlipayPayQueryRequest()
	queryReq.PaymentRequestId = orderNo

	result, err := c.apiClient.Execute(req)
	if err != nil {
		return Error, err
	}

	resp, ok := result.(*responsePay.AlipayPayQueryResponse)
	if !ok {
		return Error, errors.New("unexpected pay query response type")
	}
	if !isPayQueryResponseSuccess(resp) {
		return Error, errors.New("inquiry failed: " + responseMessage(resp))
	}

	switch resp.PaymentStatus {
	case model.TransactionStatusType_SUCCESS:
		return Success, nil
	case model.TransactionStatusType_PROCESSING:
		return Pending, nil
	case model.TransactionStatusType_FAIL:
		return Failed, nil
	case model.TransactionStatusType_CANCELLED:
		return Closed, nil
	default:
		return Error, nil
	}
}

func (c *Client) DecodeNotification(req *http.Request) (*Notification, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	ok, err := tools.CheckSignature(
		req.URL.Path,
		req.Method,
		req.Header.Get("Client-Id"),
		req.Header.Get("Request-Time"),
		string(bodyBytes),
		req.Header.Get("Signature"),
		c.AlipayPublicKey,
	)
	if err != nil || !ok {
		return nil, errors.New("signature check failed")
	}

	var notify payResultNotify
	if err := json.Unmarshal(bodyBytes, &notify); err != nil {
		return nil, err
	}

	status := mapResultStatus(notify.Result.ResultStatus)
	amount, err := parseAmountToCent(notify.PaymentAmount)
	if err != nil {
		return nil, err
	}

	return &Notification{
		OrderNo: notify.PaymentRequestId,
		Amount:  amount,
		Status:  status,
	}, nil
}

func formatAmount(amount int64) string {
	return fmt.Sprintf("%.2f", float64(amount)/100.0)
}

func parseAmountToCent(amount *model.Amount) (int64, error) {
	if amount == nil {
		return 0, nil
	}

	value, err := strconv.ParseFloat(amount.Value, 64)
	if err != nil {
		return 0, errors.Wrap(err, "invalid amount value")
	}

	return int64(value*100 + 0.5), nil
}

func mapResultStatus(status string) Status {
	switch status {
	case "S":
		return Success
	case "U":
		return Pending
	case "F":
		return Failed
	default:
		return Error
	}
}

func isPayResponseSuccess(resp *responsePay.AlipayPayResponse) bool {
	if resp == nil {
		return false
	}
	if resp.Result != nil && isAcceptedPayResult(string(resp.Result.ResultStatus), resp.Result.ResultCode) {
		return true
	}
	return isAcceptedPayResult(resp.AlipayResponse.Result.ResultStatus, resp.AlipayResponse.Result.ResultCode)
}

func isAcceptedPayResult(resultStatus, resultCode string) bool {
	return resultStatus == string(model.ResultStatusType_S) ||
		(resultStatus == string(model.ResultStatusType_U) && resultCode == "PAYMENT_IN_PROCESS")
}

func isPayQueryResponseSuccess(resp *responsePay.AlipayPayQueryResponse) bool {
	if resp == nil {
		return false
	}
	if resp.Result != nil && resp.Result.ResultStatus == model.ResultStatusType_S {
		return true
	}
	return resp.AlipayResponse.Result.ResultStatus == "S"
}

func responseMessage(resp any) string {
	switch v := resp.(type) {
	case *responsePay.AlipayPayResponse:
		if v == nil {
			return ""
		}
		if v.Result != nil && v.Result.ResultMessage != "" {
			return v.Result.ResultMessage
		}
		return v.AlipayResponse.Result.ResultMessage
	case *responsePay.AlipayPayQueryResponse:
		if v == nil {
			return ""
		}
		if v.Result != nil && v.Result.ResultMessage != "" {
			return v.Result.ResultMessage
		}
		return v.AlipayResponse.Result.ResultMessage
	default:
		return ""
	}
}

func resolvePaymentPayload(resp *responsePay.AlipayPayResponse) string {
	if resp == nil {
		return ""
	}
	if resp.NormalUrl != "" {
		return resp.NormalUrl
	}
	if resp.SchemeUrl != "" {
		return resp.SchemeUrl
	}
	if resp.ApplinkUrl != "" {
		return resp.ApplinkUrl
	}
	if resp.PaymentActionForm != "" {
		return resp.PaymentActionForm
	}
	if resp.PaymentData != "" {
		return resp.PaymentData
	}
	if resp.OrderCodeForm != nil {
		for _, detail := range resp.OrderCodeForm.CodeDetails {
			if detail != nil && detail.CodeValue != "" {
				return detail.CodeValue
			}
		}
	}
	return ""
}
