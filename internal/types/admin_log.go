package types

import "github.com/perfect-panel/server/pkg/requestmeta"

type FilterAdminActionLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type AdminActionLog struct {
	requestmeta.Metadata
	Id               int64  `json:"id,string"`
	UserId           int64  `json:"user_id,string"`
	Action           string `json:"action"`
	Object           string `json:"object,omitempty"`
	ObjectId         int64  `json:"object_id,omitempty,string"`
	Detail           string `json:"detail,omitempty"`
	Source           string `json:"source"`
	TelegramSenderId int64  `json:"telegram_sender_id,omitempty,string"`
	Timestamp        int64  `json:"timestamp"`
	CreatedAt        int64  `json:"created_at"`
}

type FilterAdminActionLogResponse struct {
	Total int64            `json:"total"`
	List  []AdminActionLog `json:"list"`
}

type FilterUnmatchedPaymentLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type UnmatchedPaymentLog struct {
	requestmeta.Metadata
	Id        int64  `json:"id,string"`
	UserId    int64  `json:"user_id,string"`
	OrderNo   string `json:"order_no"`
	TradeNo   string `json:"trade_no"`
	Platform  string `json:"platform"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
	CreatedAt int64  `json:"created_at"`
}

type FilterUnmatchedPaymentLogResponse struct {
	Total int64                 `json:"total"`
	List  []UnmatchedPaymentLog `json:"list"`
}
