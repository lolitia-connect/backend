package log

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/requestmeta"
	"go.uber.org/zap"
)

// MetadataFromContext extracts request metadata from context.
// Returns empty metadata if not found.
func MetadataFromContext(ctx context.Context) requestmeta.Metadata {
	metadata, _ := requestmeta.From(ctx)
	return metadata
}

type Type uint8

/*

Log Types:
	1X Message Logs
	2X Subscription Logs
	3X User Logs
	4X Traffic Ranking Logs
*/

const (
	TypeEmailMessage      Type = 10 // Message log
	TypeMobileMessage     Type = 11 // Mobile message log
	TypeSubscribe         Type = 20 // Subscription log
	TypeSubscribeTraffic  Type = 21 // Subscription traffic log
	TypeServerTraffic     Type = 22 // Server traffic log
	TypeResetSubscribe    Type = 23 // Reset subscription log
	TypeLogin             Type = 30 // Login log
	TypeRegister          Type = 31 // Registration log
	TypeBalance           Type = 32 // Balance log
	TypeCommission        Type = 33 // Commission log
	TypeGift              Type = 34 // Gift log
	TypeOrderCreated      Type = 35 // Order creation audit log
	TypeUnmatchedPayment  Type = 36
	TypeUserTrafficRank   Type = 40 // Top 10 User traffic rank log
	TypeServerTrafficRank Type = 41 // Top 10 Server traffic rank log
	TypeTrafficStat       Type = 42 // Daily traffic statistics log
	TypeAdminAction       Type = 50
)
const (
	ResetSubscribeTypeAuto       uint16 = 231 // Auto reset
	ResetSubscribeTypeAdvance    uint16 = 232 // Advance reset
	ResetSubscribeTypePaid       uint16 = 233 // Paid reset
	ResetSubscribeTypeQuota      uint16 = 234 // Quota reset
	BalanceTypeRecharge          uint16 = 321 // Recharge
	BalanceTypeWithdraw          uint16 = 322 // Withdraw
	BalanceTypePayment           uint16 = 323 // Payment
	BalanceTypeRefund            uint16 = 324 // Refund
	BalanceTypeAdjust            uint16 = 326 // Admin Adjust
	BalanceTypeReward            uint16 = 325 // Reward
	CommissionTypePurchase       uint16 = 331 // Purchase
	CommissionTypeRenewal        uint16 = 332 // Renewal
	CommissionTypeRefund         uint16 = 333 // Refund
	CommissionTypeWithdraw       uint16 = 334 // withdraw
	CommissionTypeAdjust         uint16 = 335 // Admin Adjust
	CommissionTypeConvertBalance uint16 = 336 // Convert to Balance
	GiftTypeIncrease             uint16 = 341 // Increase
	GiftTypeReduce               uint16 = 342 // Reduce
)

// Uint8 converts Type to uint8.
func (t Type) Uint8() uint8 {
	return uint8(t)
}

// SystemLog represents a log entry in the system.
type SystemLog struct {
	Id        int64
	Type      uint8
	Date      string
	ObjectID  int64
	Content   string
	CreatedAt time.Time
}

// UnmatchedPayment records a payment a gateway confirmed that could not settle
// its order: the order was already closed or finished, the trade differs from
// the one bound to it, or the gateway asked for manual review. It is the durable
// trace an operator refunds from.
//
// The request metadata belongs to the record because the entry exists for risk
// analysis: an administrator has to see where the callback came from.
type UnmatchedPayment struct {
	requestmeta.Metadata
	OrderNo  string `json:"order_no"`
	TradeNo  string `json:"trade_no"`
	Platform string `json:"platform"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Reason   string `json:"reason"`
}

// UnmatchedPaymentFrom returns payment carrying the request metadata of ctx, as
// recorded by the HTTP middleware. Without a metadata-bearing context the entry
// is stored as it is, so callers outside a request (tests, internal retries)
// keep working. The metadata is normalised and the free-text reason bounded, so
// an attacker-influenced callback cannot grow the row without limit.
func UnmatchedPaymentFrom(ctx context.Context, payment UnmatchedPayment) UnmatchedPayment {
	if metadata, ok := requestmeta.From(ctx); ok {
		payment.Metadata = metadata
	}
	payment.Metadata = requestmeta.Normalize(payment.Metadata)
	payment.Reason = requestmeta.Bound(payment.Reason, 2048)
	return payment
}

// DecodeUnmatchedPayment normalises a stored entry after it is unmarshalled, so
// rows written before the metadata field existed still decode and any
// out-of-bound value stored by an older writer is clamped on read.
func (u *UnmatchedPayment) Normalize() {
	if u == nil {
		return
	}
	u.Metadata = requestmeta.Normalize(u.Metadata)
	u.Reason = requestmeta.Bound(u.Reason, 2048)
}

type AdminAction struct {
	requestmeta.Metadata
	Action           string `json:"action"`
	Object           string `json:"object,omitempty"`
	ObjectID         int64  `json:"object_id,omitempty"`
	Detail           string `json:"detail,omitempty"`
	Source           string `json:"source"`
	TelegramSenderID int64  `json:"telegram_sender_id,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

func NewAdminActionLog(ctx context.Context, action AdminAction) (*SystemLog, error) {
	if metadata, ok := requestmeta.From(ctx); ok {
		actor := action.ActorID
		action.Metadata = metadata
		if actor != 0 {
			action.ActorID = actor
		}
	}
	action.Metadata = requestmeta.Normalize(action.Metadata)
	action.Detail = requestmeta.Bound(action.Detail, 2048)
	if action.Source == "" {
		action.Source = "http"
	}
	now := time.Now()
	if action.Timestamp == 0 {
		action.Timestamp = now.UnixMilli()
	}
	content, err := json.Marshal(action)
	if err != nil {
		return nil, err
	}
	return &SystemLog{Type: TypeAdminAction.Uint8(), Date: now.Format(time.DateOnly), ObjectID: action.ActorID, Content: string(content)}, nil
}

func RecordAdminAction(ctx context.Context, store Model, action AdminAction) {
	entry, err := NewAdminActionLog(ctx, action)
	if err == nil {
		err = store.Insert(ctx, entry)
	}
	if err != nil {
		zap.S().Errorw("administrator action audit failed", "action", action.Action, "object_id", action.ObjectID)
	}
}

// Message represents a message log entry.
type Message struct {
	requestmeta.Metadata
	To       string                 `json:"to"`
	Subject  string                 `json:"subject,omitempty"`
	Content  map[string]interface{} `json:"content"`
	Platform string                 `json:"platform"`
	Template string                 `json:"template"`
	Status   uint8                  `json:"status"` // 1: Sent, 2: Failed
}

// Marshal implements the json.Marshaler interface for Message.
func (m *Message) Marshal() ([]byte, error) {
	type Alias Message
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Message.
func (m *Message) Unmarshal(data []byte) error {
	type Alias Message
	aux := (*Alias)(m)
	return json.Unmarshal(data, aux)
}

// Traffic represents a subscription traffic log entry.
type Traffic struct {
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}

// Marshal implements the json.Marshaler interface for SubscribeTraffic.
func (s *Traffic) Marshal() ([]byte, error) {
	type Alias Traffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for SubscribeTraffic.
func (s *Traffic) Unmarshal(data []byte) error {
	type Alias Traffic
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// Login represents a login log entry.
type Login struct {
	requestmeta.Metadata
	Method    string `json:"method"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Login.
func (l *Login) Marshal() ([]byte, error) {
	type Alias Login
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Login.
func (l *Login) Unmarshal(data []byte) error {
	type Alias Login
	aux := (*Alias)(l)
	return json.Unmarshal(data, aux)
}

// Register represents a registration log entry.
type Register struct {
	requestmeta.Metadata
	AuthMethod string `json:"auth_method"`
	Identifier string `json:"identifier"`
	RegisterIP string `json:"register_ip"`
	UserAgent  string `json:"user_agent"`
	Timestamp  int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Register.
func (r *Register) Marshal() ([]byte, error) {
	type Alias Register
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Register.

func (r *Register) Unmarshal(data []byte) error {
	type Alias Register
	aux := (*Alias)(r)
	return json.Unmarshal(data, aux)
}

// Subscribe represents a subscription log entry.
type Subscribe struct {
	requestmeta.Metadata
	Token           string `json:"token"`
	UserAgent       string `json:"user_agent"`
	ClientIP        string `json:"client_ip"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
}

// Marshal implements the json.Marshaler interface for Subscribe.
func (s *Subscribe) Marshal() ([]byte, error) {
	type Alias Subscribe
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Subscribe.
func (s *Subscribe) Unmarshal(data []byte) error {
	type Alias Subscribe
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// ResetSubscribe represents a reset subscription log entry.
type ResetSubscribe struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	OrderNo   string `json:"order_no,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for ResetSubscribe.
func (r *ResetSubscribe) Marshal() ([]byte, error) {
	type Alias ResetSubscribe
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ResetSubscribe.
func (r *ResetSubscribe) Unmarshal(data []byte) error {
	type Alias ResetSubscribe
	aux := (*Alias)(r)
	return json.Unmarshal(data, aux)
}

// Balance represents a balance log entry.
type Balance struct {
	requestmeta.Metadata
	Type      uint16 `json:"type"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no,omitempty"`
	Balance   int64  `json:"balance"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Balance.
func (b *Balance) Marshal() ([]byte, error) {
	type Alias Balance
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(b),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Balance.
func (b *Balance) Unmarshal(data []byte) error {
	type Alias Balance
	aux := (*Alias)(b)
	return json.Unmarshal(data, aux)
}

// Commission represents a commission log entry.
type Commission struct {
	requestmeta.Metadata
	Type      uint16 `json:"type"`
	Amount    int64  `json:"amount"`
	Balance   int64  `json:"balance"`
	OrderNo   string `json:"order_no"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Commission.
func (c *Commission) Marshal() ([]byte, error) {
	type Alias Commission
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Commission.
func (c *Commission) Unmarshal(data []byte) error {
	type Alias Commission
	aux := (*Alias)(c)
	return json.Unmarshal(data, aux)
}

// Gift represents a gift log entry.
type Gift struct {
	requestmeta.Metadata
	Type        uint16 `json:"type"`
	OrderNo     string `json:"order_no"`
	SubscribeId int64  `json:"subscribe_id"`
	Amount      int64  `json:"amount"`
	Balance     int64  `json:"balance"`
	Remark      string `json:"remark,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Gift.
func (g *Gift) Marshal() ([]byte, error) {
	type Alias Gift
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(g),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Gift.
func (g *Gift) Unmarshal(data []byte) error {
	type Alias Gift
	aux := (*Alias)(g)
	return json.Unmarshal(data, aux)
}

// OrderCreated represents a durable order-creation audit entry. It stores only
// the order summary needed for operations and risk analysis; coupon codes,
// gateway trade numbers and guest credentials are deliberately excluded.
type OrderCreated struct {
	requestmeta.Metadata
	OrderNo        string `json:"order_no"`
	OrderType      uint8  `json:"order_type"`
	Quantity       int64  `json:"quantity"`
	Price          int64  `json:"price"`
	Amount         int64  `json:"amount"`
	GiftAmount     int64  `json:"gift_amount"`
	Discount       int64  `json:"discount"`
	CouponDiscount int64  `json:"coupon_discount"`
	PaymentID      int64  `json:"payment_id"`
	Method         string `json:"method"`
	FeeAmount      int64  `json:"fee_amount"`
	SubscribeID    int64  `json:"subscribe_id,omitempty"`
	Source         string `json:"source"`
	Timestamp      int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for OrderCreated.
func (o *OrderCreated) Marshal() ([]byte, error) {
	type Alias OrderCreated
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(o),
	})
}

// Unmarshal implements the json.Unmarshaler interface for OrderCreated.
func (o *OrderCreated) Unmarshal(data []byte) error {
	type Alias OrderCreated
	aux := (*Alias)(o)
	return json.Unmarshal(data, aux)
}

// UserTraffic represents a user traffic log entry.
type UserTraffic struct {
	SubscribeId int64 `json:"subscribe_id"` // Subscribe ID
	UserId      int64 `json:"user_id"`      // User ID
	Upload      int64 `json:"upload"`       // Upload traffic in bytes
	Download    int64 `json:"download"`     // Download traffic in bytes
	Total       int64 `json:"total"`        // Total traffic in bytes (Upload + Download)
}

// Marshal implements the json.Marshaler interface for UserTraffic.
func (u *UserTraffic) Marshal() ([]byte, error) {
	type Alias UserTraffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(u),
	})
}

// Unmarshal implements the json.Unmarshaler interface for UserTraffic.
func (u *UserTraffic) Unmarshal(data []byte) error {
	type Alias UserTraffic
	aux := (*Alias)(u)
	return json.Unmarshal(data, aux)
}

// UserTrafficRank represents a user traffic rank entry.
type UserTrafficRank struct {
	Rank map[uint8]UserTraffic `json:"rank"` // Key is rank ,type is UserTraffic
}

// Marshal implements the json.Marshaler interface for UserTrafficRank.
func (u *UserTrafficRank) Marshal() ([]byte, error) {
	type Alias UserTrafficRank
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(u),
	})
}

// Unmarshal implements the json.Unmarshaler interface for UserTrafficRank.
func (u *UserTrafficRank) Unmarshal(data []byte) error {
	type Alias UserTrafficRank
	aux := (*Alias)(u)
	return json.Unmarshal(data, aux)
}

// ServerTraffic represents a server traffic log entry.
type ServerTraffic struct {
	ServerId int64 `json:"server_id"` // Server ID
	Upload   int64 `json:"upload"`    // Upload traffic in bytes
	Download int64 `json:"download"`  // Download traffic in bytes
	Total    int64 `json:"total"`     // Total traffic in bytes (Upload + Download)
}

// Marshal implements the json.Marshaler interface for ServerTraffic.
func (s *ServerTraffic) Marshal() ([]byte, error) {
	type Alias ServerTraffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ServerTraffic.
func (s *ServerTraffic) Unmarshal(data []byte) error {
	type Alias ServerTraffic
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// ServerTrafficRank represents a server traffic rank entry.
type ServerTrafficRank struct {
	Rank map[uint8]ServerTraffic `json:"rank"` // Key is rank ,type is ServerTraffic
}

// Marshal implements the json.Marshaler interface for ServerTrafficRank.
func (s *ServerTrafficRank) Marshal() ([]byte, error) {
	type Alias ServerTrafficRank
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ServerTrafficRank.
func (s *ServerTrafficRank) Unmarshal(data []byte) error {
	type Alias ServerTrafficRank
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// TrafficStat represents a daily traffic statistics log entry.
type TrafficStat struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
}

// Marshal implements the json.Marshaler interface for TrafficStat.
func (t *TrafficStat) Marshal() ([]byte, error) {
	type Alias TrafficStat
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(t),
	})
}

// Unmarshal implements the json.Unmarshaler interface for TrafficStat.
func (t *TrafficStat) Unmarshal(data []byte) error {
	type Alias TrafficStat
	aux := (*Alias)(t)
	return json.Unmarshal(data, aux)
}
