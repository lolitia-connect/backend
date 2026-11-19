package epay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var scalarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var integerValue = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

type GatewayConfig struct {
	Version      string
	CheckoutMode string
	Endpoint     string
	Domain       string
	MerchantID   string
	Key          string
	PrivateKey   string
	PublicKey    string
	PaymentType  string
	HTTPClient   *http.Client
}

type Gateway struct {
	config     GatewayConfig
	endpoint   string
	legacy     *Client
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	http       *http.Client
	now        func() time.Time
}

func Open(config GatewayConfig) (*Gateway, error) {
	config.Version = strings.ToLower(strings.TrimSpace(config.Version))
	config.CheckoutMode = strings.ToLower(strings.TrimSpace(config.CheckoutMode))
	if config.CheckoutMode != "" && config.CheckoutMode != "page" && config.CheckoutMode != "api" {
		return nil, errors.New("EPay checkout_mode must be page or api")
	}
	if config.CheckoutMode == "api" && config.PaymentType == "" {
		return nil, errors.New("EPay API checkout requires a payment type")
	}
	endpoint, err := ResolveEndpoint(config.Endpoint, config.Domain)
	if err != nil {
		return nil, err
	}
	httpClient := http.Client{Timeout: 15 * time.Second}
	if config.HTTPClient != nil {
		httpClient = *config.HTTPClient
	}
	if httpClient.Timeout == 0 {
		httpClient.Timeout = 15 * time.Second
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	gateway := &Gateway{config: config, endpoint: strings.TrimRight(endpoint, "/"), http: &httpClient, now: time.Now}
	switch config.Version {
	case "", "v1":
		if config.PrivateKey != "" || config.PublicKey != "" {
			return nil, errors.New("EPay RSA keys require version v2")
		}
		if strings.TrimSpace(config.MerchantID) == "" || strings.TrimSpace(config.Key) == "" {
			return nil, errors.New("EPay merchant and shared key are required")
		}
		gateway.legacy = NewClient(config.MerchantID, endpoint, config.Key, config.PaymentType)
		gateway.legacy.httpClient = &httpClient
	case "v2":
		if config.Key != "" {
			return nil, errors.New("EPay V2 requires RSA keys instead of a shared key")
		}
		if config.MerchantID == "" || strings.Trim(config.MerchantID, "0123456789") != "" {
			return nil, errors.New("EPay V2 merchant ID must contain decimal digits")
		}
		parsed, _ := url.Parse(endpoint)
		if parsed.Scheme != "https" {
			return nil, errors.New("EPay V2 requires HTTPS")
		}
		if config.PaymentType != "" && config.PaymentType != "alipay" && config.PaymentType != "wxpay" && config.PaymentType != "qqpay" {
			return nil, errors.New("unsupported EPay V2 payment type")
		}
		gateway.privateKey, err = parseMerchantKey(config.PrivateKey)
		if err != nil {
			return nil, err
		}
		gateway.publicKey, err = parsePlatformKey(config.PublicKey)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("EPay version must be v1 or v2")
	}
	return gateway, nil
}

func keyBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "-----BEGIN ") {
		block, rest := pem.Decode([]byte(value))
		if block == nil || len(block.Headers) != 0 || strings.TrimSpace(string(rest)) != "" {
			return nil, errors.New("expected a single unencrypted PEM key")
		}
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
}

func parseMerchantKey(value string) (*rsa.PrivateKey, error) {
	encoded, err := keyBytes(value)
	if err != nil {
		return nil, errors.New("invalid EPay merchant private key")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(encoded); err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else {
		key, _ = x509.ParsePKCS1PrivateKey(encoded)
	}
	if key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, errors.New("EPay merchant private key must be RSA with at least 2048 bits")
	}
	return key, nil
}

func parsePlatformKey(value string) (*rsa.PublicKey, error) {
	encoded, err := keyBytes(value)
	if err != nil {
		return nil, errors.New("invalid EPay platform public key")
	}
	var key *rsa.PublicKey
	if parsed, err := x509.ParsePKIXPublicKey(encoded); err == nil {
		key, _ = parsed.(*rsa.PublicKey)
	} else {
		key, _ = x509.ParsePKCS1PublicKey(encoded)
	}
	if key == nil || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
		return nil, errors.New("EPay platform public key must be RSA with at least 2048 bits")
	}
	return key, nil
}

func canonicalV2(params map[string]string) (string, error) {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if !scalarName.MatchString(name) {
			return "", fmt.Errorf("invalid EPay scalar parameter %q", name)
		}
		if name != "sign" && name != "sign_type" && strings.Trim(value, " \t\n\r\x00\x0b") != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		parts = append(parts, name+"="+params[name])
	}
	return strings.Join(parts, "&"), nil
}

func signV2(key *rsa.PrivateKey, params map[string]string) (string, error) {
	canonical, err := canonicalV2(params)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	return base64.StdEncoding.EncodeToString(signature), err
}

func (gateway *Gateway) signed(params map[string]string) (url.Values, error) {
	values := make(map[string]string, len(params)+4)
	for name, value := range params {
		if name == "pid" || name == "key" || name == "timestamp" || name == "sign" || name == "sign_type" {
			return nil, fmt.Errorf("EPay client owns parameter %s", name)
		}
		values[name] = value
	}
	values["pid"] = gateway.config.MerchantID
	if gateway.legacy != nil {
		values["sign_type"] = "MD5"
		values["sign"] = gateway.legacy.createSign(values)
	} else {
		values["timestamp"] = strconv.FormatInt(gateway.now().Unix(), 10)
		values["sign_type"] = "RSA"
		signature, err := signV2(gateway.privateKey, values)
		if err != nil {
			return nil, err
		}
		values["sign"] = signature
	}
	form := make(url.Values, len(values))
	for name, value := range values {
		form.Set(name, value)
	}
	return form, nil
}

func (gateway *Gateway) verifyV2(params map[string]string) error {
	if params["sign_type"] != "RSA" {
		return errors.New("invalid EPay signature type")
	}
	canonical, err := canonicalV2(params)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(params["sign"])
	if err != nil || len(signature) == 0 {
		return errors.New("invalid EPay signature")
	}
	digest := sha256.Sum256([]byte(canonical))
	if rsa.VerifyPKCS1v15(gateway.publicKey, crypto.SHA256, digest[:], signature) != nil {
		return errors.New("EPay signature verification failed")
	}
	stamp := params["timestamp"]
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || len(stamp) != 10 || strings.Trim(stamp, "0123456789") != "" {
		return errors.New("invalid EPay timestamp")
	}
	delta := gateway.now().Unix() - seconds
	if delta < -300 || delta > 300 {
		return errors.New("EPay signature timestamp expired")
	}
	if merchant, present := params["pid"]; present && merchant != gateway.config.MerchantID {
		return errors.New("EPay merchant ID mismatch")
	}
	return nil
}

func responseFields(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("EPay response is not an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid EPay response field")
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("duplicate EPay response field %s", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("invalid EPay response value")
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("unexpected trailing EPay response data")
	}
	return fields, nil
}

func responseScalars(fields map[string]json.RawMessage) (map[string]string, error) {
	params := make(map[string]string)
	for name, raw := range fields {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			return nil, errors.New("empty EPay response value")
		}
		switch raw[0] {
		case '{', '[':
			continue
		case '"':
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return nil, err
			}
			params[name] = text
		default:
			switch string(raw) {
			case "null", "false":
				params[name] = ""
			case "true":
				params[name] = "1"
			default:
				if !integerValue.Match(raw) {
					return nil, errors.New("EPay response contains non-integer scalar")
				}
				params[name] = string(raw)
			}
		}
	}
	return params, nil
}

func (gateway *Gateway) call(ctx context.Context, apiPath string, params map[string]string) (map[string]string, error) {
	form, err := gateway.signed(params)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.endpoint+apiPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("invalid EPay API request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := gateway.http.Do(request)
	if err != nil {
		return nil, errors.New("EPay request outcome requires reconciliation")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("EPay API returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, errors.New("invalid EPay response size")
	}
	fields, err := responseFields(body)
	if err != nil {
		return nil, err
	}
	values, err := responseScalars(fields)
	if err != nil {
		return nil, err
	}
	code, present := fields["code"]
	if !present || !integerValue.Match(code) {
		return nil, errors.New("EPay response has no integer code")
	}
	success := string(code) == "0"
	if gateway.legacy != nil {
		success = string(code) == "1"
	}
	if gateway.legacy == nil && (success || values["sign"] != "") {
		if err := gateway.verifyV2(values); err != nil {
			return nil, err
		}
	}
	if !success {
		return nil, errors.New("EPay API rejected operation; reconcile before retrying")
	}
	return values, nil
}

type PaymentSession struct {
	State   string `json:"state,omitempty"`
	TradeNo string `json:"trade_no"`
	PayType string `json:"pay_type"`
	PayInfo string `json:"pay_info"`
}

type OrderReceipt struct {
	MerchantID string
	TradeNo    string
	OrderNo    string
	Type       string
	Money      string
	Paid       bool
	Unpaid     bool
	Refunded   bool
	StatusOnly bool
}

func (gateway *Gateway) CreatePayment(ctx context.Context, name, orderNo, money, notifyURL, returnURL, clientIP string) (*PaymentSession, error) {
	if gateway.config.CheckoutMode != "api" {
		return nil, errors.New("EPay API checkout is disabled")
	}
	if err := gateway.ValidatePayment(name, orderNo, money, notifyURL, returnURL, clientIP); err != nil {
		return nil, err
	}
	params := map[string]string{"type": gateway.config.PaymentType, "out_trade_no": orderNo, "name": name, "money": money, "notify_url": notifyURL, "return_url": returnURL, "clientip": clientIP}
	if gateway.legacy != nil {
		params["device"] = "jump"
		result, err := gateway.call(ctx, "/mapi.php", params)
		if err != nil {
			return nil, err
		}
		session := &PaymentSession{TradeNo: result["trade_no"]}
		entries := 0
		for _, field := range []struct{ name, kind string }{{"payurl", "jump"}, {"qrcode", "qrcode"}, {"urlscheme", "urlscheme"}} {
			if strings.TrimSpace(result[field.name]) != "" {
				entries++
				session.PayType, session.PayInfo = field.kind, result[field.name]
			}
		}
		if entries != 1 || session.TradeNo == "" {
			return nil, errors.New("EPay V1 response must contain one payment entry and a trade number")
		}
		return session, nil
	}
	params["method"] = "jump"
	values, err := gateway.call(ctx, "/api/pay/create", params)
	if err != nil {
		return nil, err
	}
	if values["trade_no"] == "" || values["pay_type"] == "" || values["pay_info"] == "" {
		return nil, errors.New("EPay V2 response lacks payment session")
	}
	return &PaymentSession{TradeNo: values["trade_no"], PayType: values["pay_type"], PayInfo: values["pay_info"]}, nil
}

func (gateway *Gateway) ValidatePayment(name, orderNo, money, notifyURL, returnURL, clientIP string) error {
	if strings.TrimSpace(name) == "" || len(name) > 127 || strings.TrimSpace(orderNo) != orderNo || orderNo == "" || len(orderNo) > 255 {
		return errors.New("invalid EPay order name or number")
	}
	amount, err := ParseMoney(money)
	if err != nil || amount <= 0 {
		return errors.New("invalid EPay payment amount")
	}
	addresses := []string{notifyURL}
	if returnURL != "" {
		addresses = append(addresses, returnURL)
	}
	for _, address := range addresses {
		parsed, err := url.Parse(address)
		if err != nil || parsed.User != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Fragment != "" {
			return errors.New("invalid EPay callback URL")
		}
	}
	if gateway.config.CheckoutMode == "api" && (gateway.config.PaymentType == "" || net.ParseIP(clientIP) == nil) {
		return errors.New("EPay API checkout requires a payment type and valid client IP")
	}
	return nil
}

func (gateway *Gateway) PagePayment(name, orderNo, money, notifyURL, returnURL string) (string, error) {
	if err := gateway.ValidatePayment(name, orderNo, money, notifyURL, returnURL, ""); err != nil {
		return "", err
	}
	if gateway.legacy != nil {
		amount, _ := ParseMoney(money)
		return gateway.legacy.CreatePayUrl(Order{Name: name, OrderNo: orderNo, AmountMinor: amount, NotifyUrl: notifyURL, ReturnUrl: returnURL})
	}
	if returnURL == "" {
		return "", errors.New("EPay V2 page checkout requires a return URL")
	}
	form, err := gateway.signed(map[string]string{"type": gateway.config.PaymentType, "out_trade_no": orderNo, "name": name, "money": money, "notify_url": notifyURL, "return_url": returnURL})
	if err != nil {
		return "", err
	}
	return gateway.endpoint + "/api/pay/submit?" + form.Encode(), nil
}

func (gateway *Gateway) QueryOrder(ctx context.Context, orderNo, tradeNo string) (*OrderReceipt, error) {
	if orderNo == "" && tradeNo == "" {
		return nil, errors.New("EPay order reference is required")
	}
	if gateway.legacy != nil {
		result, err := gateway.legacy.QueryOrder(orderNo)
		if err != nil {
			return nil, err
		}
		if result.StatusOnly {
			return &OrderReceipt{Paid: result.Paid, Unpaid: result.Unpaid, StatusOnly: true}, nil
		}
		if tradeNo != "" && result.TradeNo != tradeNo {
			return nil, errors.New("EPay receipt has a different trade number")
		}
		receipt := &OrderReceipt{MerchantID: result.MerchantID, TradeNo: result.TradeNo, OrderNo: result.OrderNo, Type: result.Type, Money: result.Money, Paid: result.Paid, Unpaid: result.Unpaid}
		return receipt, gateway.validateReceipt(receipt, orderNo, tradeNo)
	}
	ref := map[string]string{"out_trade_no": orderNo}
	if tradeNo != "" {
		ref = map[string]string{"trade_no": tradeNo}
	}
	values, err := gateway.call(ctx, "/api/pay/query", ref)
	if err != nil {
		return nil, err
	}
	status, err := strconv.Atoi(values["status"])
	if err != nil || status < 0 || status > 4 {
		return nil, errors.New("EPay order response has invalid status")
	}
	refunded := int64(0)
	if values["refundmoney"] != "" {
		refunded, err = ParseMoney(values["refundmoney"])
		if err != nil {
			return nil, errors.New("invalid EPay refunded amount")
		}
	}
	receipt := &OrderReceipt{MerchantID: values["pid"], TradeNo: values["trade_no"], OrderNo: values["out_trade_no"], Type: values["type"], Money: values["money"], Paid: status == 1, Unpaid: status == 0, Refunded: refunded != 0 || status == 2}
	return receipt, gateway.validateReceipt(receipt, orderNo, tradeNo)
}

func (gateway *Gateway) validateReceipt(receipt *OrderReceipt, orderNo, tradeNo string) error {
	if receipt.MerchantID != gateway.config.MerchantID || receipt.TradeNo == "" || receipt.OrderNo == "" || (orderNo != "" && receipt.OrderNo != orderNo) || (tradeNo != "" && receipt.TradeNo != tradeNo) || receipt.Type == "" || (gateway.config.PaymentType != "" && receipt.Type != gateway.config.PaymentType) {
		return errors.New("EPay receipt identity does not match the request")
	}
	amount, err := ParseMoney(receipt.Money)
	if err != nil || amount <= 0 {
		return errors.New("EPay receipt has invalid money")
	}
	return nil
}

func (gateway *Gateway) CloseOrder(ctx context.Context, orderNo, tradeNo string) error {
	if gateway.legacy != nil {
		return errors.New("EPay V1 has no authenticated close API")
	}
	if orderNo == "" && tradeNo == "" {
		return errors.New("EPay order reference is required")
	}
	ref := map[string]string{"out_trade_no": orderNo}
	if tradeNo != "" {
		ref = map[string]string{"trade_no": tradeNo}
	}
	_, err := gateway.call(ctx, "/api/pay/close", ref)
	return err
}

func (gateway *Gateway) VerifyNotification(values url.Values) (map[string]string, error) {
	params := make(map[string]string, len(values))
	for name, items := range values {
		if len(items) != 1 {
			return nil, fmt.Errorf("EPay notification parameter %s must occur once", name)
		}
		params[name] = items[0]
	}
	if gateway.legacy != nil {
		for name := range params {
			if !scalarName.MatchString(name) {
				return nil, errors.New("invalid EPay notification parameter")
			}
		}
		if !strings.EqualFold(params["sign_type"], "MD5") || !gateway.legacy.VerifySign(params) {
			return nil, errors.New("EPay signature verification failed")
		}
	} else if err := gateway.verifyV2(params); err != nil {
		return nil, err
	}
	if params["pid"] != gateway.config.MerchantID || params["trade_status"] != "TRADE_SUCCESS" || params["trade_no"] == "" || params["out_trade_no"] == "" || params["type"] == "" || params["money"] == "" {
		return nil, errors.New("invalid EPay notification")
	}
	if gateway.config.PaymentType != "" && params["type"] != gateway.config.PaymentType {
		return nil, errors.New("EPay notification payment type mismatch")
	}
	if amount, err := ParseMoney(params["money"]); err != nil || amount <= 0 {
		return nil, errors.New("invalid EPay notification money")
	}
	return params, nil
}

func (session *PaymentSession) CheckoutURL() (string, string, error) {
	if session == nil || strings.TrimSpace(session.PayInfo) == "" {
		return "", "", errors.New("EPay payment session is empty")
	}
	switch session.PayType {
	case "qrcode":
		return "qr", session.PayInfo, nil
	case "jump", "urlscheme":
		u, err := url.Parse(session.PayInfo)
		if err != nil || u.User != nil {
			return "", "", errors.New("invalid EPay checkout URL")
		}
		valid := (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
		if session.PayType == "urlscheme" {
			valid = u.Scheme == "alipay" || u.Scheme == "alipays" || u.Scheme == "weixin" || u.Scheme == "mqqapi" || u.Scheme == "mqqwallet"
		}
		if !valid {
			return "", "", errors.New("unsupported EPay checkout URL")
		}
		return "url", session.PayInfo, nil
	default:
		return "", "", fmt.Errorf("EPay payment type %q requires dedicated client support", session.PayType)
	}
}
