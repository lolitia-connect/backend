package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	publicOrder "github.com/perfect-panel/server/internal/handler/public/order"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
)

// RegisterV2OrderHandlers wires the V2 checkout surface. It is hand-written
// rather than generated, because the route table is generated from the *.api
// files and this surface has two properties the generator cannot express: an
// optional authentication mode, and one route that must bypass the native-device
// middleware entirely.
func RegisterV2OrderHandlers(router *server.Hertz, serverCtx *svc.ServiceContext) {
	group := router.Group("/v2/public/orders")
	group.Use(hertzx.Wrap(middleware.OptionalAuthMiddleware(serverCtx)), hertzx.Wrap(middleware.DeviceMiddleware(serverCtx)))
	group.POST("", hertzx.Wrap(publicOrder.V2CreateAndCheckoutHandler(serverCtx)))
	group.POST("/:orderNo/checkout", hertzx.Wrap(publicOrder.V2CheckoutHandler(serverCtx)))
	group.GET("/:orderNo", hertzx.Wrap(publicOrder.V2GetOrderHandler(serverCtx)))
	group.POST("/:orderNo/event-ticket", hertzx.Wrap(publicOrder.V2EventTicketHandler(serverCtx)))
	group.POST("/:orderNo/session", hertzx.Wrap(publicOrder.V2OrderSessionHandler(serverCtx)))

	// EventSource cannot participate in the native-device response encryption
	// protocol. The short-lived stream ticket is the authorization mechanism for
	// this browser-facing route, so do not apply DeviceMiddleware here.
	streamGroup := router.Group("/v2/public/orders")
	streamGroup.Use(hertzx.Wrap(middleware.OptionalAuthMiddleware(serverCtx)))
	streamGroup.GET("/:orderNo/events", hertzx.Wrap(publicOrder.V2OrderEventsHandler(serverCtx)))
}
