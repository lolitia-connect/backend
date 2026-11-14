package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stdErrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/protocol/sse"
	orderLogic "github.com/perfect-panel/server/internal/logic/public/order"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const v2SSEMaxConnectionsPerTicket = 3

// V2CreateAndCheckoutHandler combines order creation and checkout initiation.
// The idempotency key is intentionally a header so browser retry middleware can
// preserve it independently from a JSON request body.
func V2CreateAndCheckoutHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if !validIdempotencyKey(idempotencyKey) {
			result.ParamErrorResult(c, stdErrors.New("Idempotency-Key must contain 16-128 printable ASCII characters"))
			return
		}
		var req types.V2CreateOrderRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx).CreateAndCheckout(&req, idempotencyKey)
		if stdErrors.Is(err, orderLogic.ErrIdempotencyKeyReused) {
			c.JSON(http.StatusConflict, result.Error(xerr.InvalidParams, "IDEMPOTENCY_KEY_REUSED"))
			return
		}
		result.HttpResult(c, resp, err)
	}
}

// V2CheckoutHandler re-initiates checkout for a pending V2 order.
func V2CheckoutHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.V2CheckoutOrderRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx).Checkout(c.Param("orderNo"), &req)
		result.HttpResult(c, resp, err)
	}
}

// V2GetOrderHandler returns the current state snapshot of one order.
func V2GetOrderHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		resp, err := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx).GetOrder(c.Param("orderNo"), c.Query("checkout_token"))
		result.HttpResult(c, resp, err)
	}
}

// V2EventTicketHandler refreshes the short-lived stream ticket.
func V2EventTicketHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.V2EventTicketRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx).EventTicket(c.Param("orderNo"), req.CheckoutToken)
		result.HttpResult(c, resp, err)
	}
}

// V2OrderSessionHandler exchanges a guest checkout capability for a user session.
func V2OrderSessionHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.V2OrderSessionRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx).Session(c.Param("orderNo"), req.CheckoutToken)
		result.HttpResult(c, resp, err)
	}
}

// V2OrderEventsHandler serves a replayable SSE stream. The event table is the
// source of truth; Redis is only used to wake the handler quickly after an
// outbox publication. A periodic database catch-up keeps streams correct if
// Redis or a subscription is briefly unavailable.
func V2OrderEventsHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		logic := orderLogic.NewV2OrderLogic(c.Request.Context(), svcCtx)
		orderNo := c.Param("orderNo")
		ticket := c.Query("ticket")
		orderInfo, err := logic.AuthorizeEventTicket(orderNo, ticket)
		if err != nil {
			result.HttpResult(c, nil, err)
			return
		}
		expiresAt, err := logic.EventTicketExpiresAt(ticket)
		if err != nil {
			result.HttpResult(c, nil, err)
			return
		}
		release, allowed := acquireSSEConnection(c, svcCtx.Redis, ticket, time.Until(expiresAt))
		if !allowed {
			c.JSON(http.StatusTooManyRequests, result.Error(xerr.TooManyRequests, "too many concurrent SSE connections"))
			return
		}
		defer release()

		// Headers must be set on the hertz context: this handler writes the
		// response itself, so nothing reaches the wrapper's header map.
		raw := c.Raw()
		raw.Header("X-Accel-Buffering", "no")
		raw.Header("Cache-Control", "no-cache")
		writer := sse.NewWriter(raw)
		defer func() { _ = writer.Close() }()

		// Subscribe before querying the event table. Query and broadcast can
		// overlap, but the monotonically increasing event id makes that safe.
		pubsub, messages := subscribeOrderEvents(c, svcCtx.Redis, orderNo)
		if pubsub != nil {
			defer func() { _ = pubsub.Close() }()
		}

		if err := writeSSESnapshot(writer, logic.Snapshot(orderInfo)); err != nil {
			return
		}
		afterID := requestedEventID(c)
		if afterID > 0 {
			earliestID, err := svcCtx.Store.OrderEvent().EarliestID(c, orderNo)
			if err != nil {
				zap.S().Errorw("[V2OrderEvents] inspect replay cursor failed", zap.Any("error", err.Error()), zap.Any("order_no", orderNo))
			} else if earliestID > afterID {
				// The requested cursor predates the retained window, so the
				// client's local state cannot be reconstructed by replay alone.
				if err := writeSSEReset(writer, logic.Snapshot(orderInfo)); err != nil {
					return
				}
				afterID = earliestID - 1
			}
		}
		if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
			zap.S().Errorw("[V2OrderEvents] initial replay failed", zap.Any("error", err.Error()), zap.Any("order_no", orderNo))
		}

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		catchUp := time.NewTicker(5 * time.Second)
		defer catchUp.Stop()
		resubscribe := time.NewTicker(5 * time.Second)
		defer resubscribe.Stop()
		expiration := time.NewTimer(time.Until(expiresAt))
		defer expiration.Stop()

		for {
			select {
			case <-c.Done():
				return
			case <-expiration.C:
				data, _ := json.Marshal(map[string]string{"reason": "ticket_expired"})
				_ = writer.WriteEvent("", "stream.expiring", data)
				return
			case _, ok := <-messages:
				if !ok {
					messages = nil
					if pubsub != nil {
						_ = pubsub.Close()
						pubsub = nil
					}
					continue
				}
				if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
					return
				}
			case <-catchUp.C:
				if err := replayOrderEvents(c, writer, svcCtx, orderNo, &afterID); err != nil {
					return
				}
			case <-heartbeat.C:
				if err := writer.WriteKeepAlive(); err != nil {
					return
				}
			case <-resubscribe.C:
				if messages == nil {
					pubsub, messages = subscribeOrderEvents(c, svcCtx.Redis, orderNo)
				}
			}
		}
	}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 16 || len(key) > 128 {
		return false
	}
	for _, char := range []byte(key) {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

// requestedEventID prefers the browser-managed Last-Event-ID header and falls
// back to an explicit cursor for clients that cannot set it.
func requestedEventID(c *hertzx.Context) int64 {
	value := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if value == "" {
		value = strings.TrimSpace(c.Query("after"))
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func writeSSESnapshot(writer *sse.Writer, snapshot types.V2OrderSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return writer.WriteEvent("", "order.snapshot", data)
}

// writeSSEReset tells the client that its cursor is older than the retained
// window and that the snapshot it just received is authoritative.
func writeSSEReset(writer *sse.Writer, snapshot types.V2OrderSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return writer.WriteEvent("", "order.reset", data)
}

func replayOrderEvents(ctx context.Context, writer *sse.Writer, svcCtx *svc.ServiceContext, orderNo string, afterID *int64) error {
	for {
		events, err := svcCtx.Store.OrderEvent().ListAfter(ctx, orderNo, *afterID, 500)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Id <= *afterID {
				continue
			}
			if err := writer.WriteEvent(strconv.FormatInt(event.Id, 10), event.EventType, []byte(event.Payload)); err != nil {
				return err
			}
			*afterID = event.Id
		}
		if len(events) < 500 {
			return nil
		}
	}
}

func subscribeOrderEvents(ctx context.Context, client *redis.Client, orderNo string) (*redis.PubSub, <-chan *redis.Message) {
	if client == nil {
		return nil, nil
	}
	pubsub := client.Subscribe(ctx, orderstream.Channel(orderNo))
	confirmCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := pubsub.Receive(confirmCtx); err != nil {
		_ = pubsub.Close()
		return nil, nil
	}
	return pubsub, pubsub.Channel()
}

// acquireSSEConnection caps concurrent streams per ticket. A ticketed stream is
// still served when Redis is unavailable: the expiry guard is a best-effort
// abuse control, not a reason to hide a paid order from its owner.
func acquireSSEConnection(ctx context.Context, client *redis.Client, ticket string, ttl time.Duration) (func(), bool) {
	if client == nil {
		return func() {}, true
	}
	digest := sha256.Sum256([]byte(ticket))
	key := "order:sse:connections:" + hex.EncodeToString(digest[:])
	count, err := client.Incr(ctx, key).Result()
	if err != nil {
		return func() {}, true
	}
	if count == 1 {
		if ttl < time.Minute {
			ttl = time.Minute
		}
		_ = client.Expire(ctx, key, ttl).Err()
	}
	if count > v2SSEMaxConnectionsPerTicket {
		_, _ = client.Decr(ctx, key).Result()
		return func() {}, false
	}
	return func() {
		_, _ = client.Decr(context.Background(), key).Result()
	}, true
}
