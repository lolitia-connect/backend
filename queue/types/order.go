package types

const (
	DeferCloseOrder        = "defer:order:close"
	ForthwithActivateOrder = "forthwith:order:activate"
	PublishOrderEvents     = "order:events:publish"
	CleanupOrderEvents     = "order:events:cleanup"
	RecoverStripeOrders    = "order:stripe:recover"
)

type (
	DeferCloseOrderPayload struct {
		OrderNo string `json:"order_no"`
	}
	ForthwithActivateOrderPayload struct {
		OrderNo string `json:"order_no"`
	}
)
