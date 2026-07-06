package payment

import (
	"encoding/json"
)

type Payment struct {
	Id           int64
	Name         string
	Platform     string
	Icon         string
	Domain       string
	Config       string
	Description  string
	FeeMode      uint
	FeePercent   int64
	FeeAmount    int64
	Sort         int64
	Enable       *bool
	Token        string
	CurrencyUnit string
	ExchangeRate float64
	BillDesc     string
}

type Filter struct {
	Mark   string
	Enable *bool
	Search string
}

type StripeConfig struct {
	PublicKey     string `json:"public_key"`
	SecretKey     string `json:"secret_key"`
	WebhookSecret string `json:"webhook_secret"`
	Payment       string `json:"payment"`
}

func (l *StripeConfig) Marshal() ([]byte, error) {
	type Alias StripeConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *StripeConfig) Unmarshal(data []byte) error {
	type Alias StripeConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

type AlipayF2FConfig struct {
	AppId       string `json:"app_id"`
	PrivateKey  string `json:"private_key"`
	PublicKey   string `json:"public_key"`
	InvoiceName string `json:"invoice_name"`
	Sandbox     bool   `json:"sandbox"`
}

func (l *AlipayF2FConfig) Marshal() ([]byte, error) {
	type Alias AlipayF2FConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *AlipayF2FConfig) Unmarshal(data []byte) error {
	// First try to unmarshal into a map to handle string "true"/"false" for sandbox
	var rawMap map[string]interface{}
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	// Convert sandbox field if it's a string
	if sandboxVal, ok := rawMap["sandbox"]; ok {
		switch v := sandboxVal.(type) {
		case string:
			rawMap["sandbox"] = v == "true" || v == "1"
		case bool:
			// Already a bool, no conversion needed
		}
	}

	// Re-marshal and unmarshal into the struct
	convertedData, err := json.Marshal(rawMap)
	if err != nil {
		return err
	}
	type Alias AlipayF2FConfig
	return json.Unmarshal(convertedData, (*Alias)(l))
}

type EPayConfig struct {
	Pid  string `json:"pid"`
	Url  string `json:"url"`
	Key  string `json:"key"`
	Type string `json:"type"`
}

func (l *EPayConfig) Marshal() ([]byte, error) {
	type Alias EPayConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *EPayConfig) Unmarshal(data []byte) error {
	type Alias EPayConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

type CryptoSaaSConfig struct {
	Endpoint  string `json:"endpoint"`
	AccountID string `json:"account_id"`
	SecretKey string `json:"secret_key"`
	Type      string `json:"type"`
}

func (l *CryptoSaaSConfig) Marshal() ([]byte, error) {
	type Alias CryptoSaaSConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *CryptoSaaSConfig) Unmarshal(data []byte) error {
	type Alias CryptoSaaSConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

type AlipayPlusConfig struct {
	ClientId        string `json:"client_id"`
	MerchantId      string `json:"merchant_id"`
	PrivateKey      string `json:"private_key"`
	AlipayPublicKey string `json:"alipay_public_key"`
	GatewayUrl      string `json:"gateway_url"`
	PaymentMethod   string `json:"payment_method"`
	NotifyURL       string `json:"notify_url"`
	RedirectURL     string `json:"redirect_url"`
}

func (l *AlipayPlusConfig) Marshal() ([]byte, error) {
	type Alias AlipayPlusConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *AlipayPlusConfig) Unmarshal(data []byte) error {
	type Alias AlipayPlusConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}
