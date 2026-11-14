package portal

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"

	"github.com/perfect-panel/server/pkg/tool"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryPurchaseOrderLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryPurchaseOrderLogic Query Purchase Order
func NewQueryPurchaseOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryPurchaseOrderLogic {
	return &QueryPurchaseOrderLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Centralized error handler for database issues
func wrapDatabaseError(err error) error {
	return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Database Query Error: %v", err.Error())
}

func (l *QueryPurchaseOrderLogic) QueryPurchaseOrder(req *types.QueryPurchaseOrderRequest) (resp *types.QueryPurchaseOrderResponse, err error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		return nil, wrapDatabaseError(err)
	}
	if err := l.authorizePurchaseOrder(orderInfo, req); err != nil {
		return nil, err
	}
	// Handle temporary orders if applicable
	var token string
	if orderInfo.Status == 2 || orderInfo.Status == 5 {
		if token, err = l.handleTemporaryOrder(orderInfo); err != nil {
			return nil, err
		}
	}
	// Fetch subscription and payment information
	subscribeInfo, paymentInfo, err := l.fetchOrderDetails(orderInfo)
	if err != nil {
		return nil, err
	}

	return &types.QueryPurchaseOrderResponse{
		OrderNo:        orderInfo.OrderNo,
		Subscribe:      subscribeInfo,
		Quantity:       orderInfo.Quantity,
		Price:          orderInfo.Price,
		Amount:         orderInfo.Amount,
		Discount:       orderInfo.Discount,
		Coupon:         orderInfo.Coupon,
		CouponDiscount: orderInfo.CouponDiscount,
		FeeAmount:      orderInfo.FeeAmount,
		Payment:        paymentInfo,
		Status:         orderInfo.Status,
		CreatedAt:      orderInfo.CreatedAt.UnixMilli(),
		Token:          token,
	}, nil
}

// authorizePurchaseOrder accepts either the authenticated owner of a completed
// guest order or the unguessable checkout capability issued when that order was
// created. An email/identifier is not authentication and must never be used to
// mint a session token.
func (l *QueryPurchaseOrderLogic) authorizePurchaseOrder(orderInfo *order.Order, req *types.QueryPurchaseOrderRequest) error {
	if orderInfo.UserId != 0 {
		if currentUser, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User); ok && currentUser.Id == orderInfo.UserId {
			return nil
		}
	}
	if req.CheckoutToken == "" {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is required")
	}
	if orderInfo.GuestCheckoutTokenHash != "" {
		if subtle.ConstantTimeCompare([]byte(orderInfo.GuestCheckoutTokenHash), []byte(constant.CheckoutTokenHash(req.CheckoutToken))) != 1 {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
		}
		return nil
	}
	// Compatibility for orders created before the capability was persisted on
	// the order itself.
	cacheKey := fmt.Sprintf(constant.TempOrderCacheKey, orderInfo.OrderNo)
	cacheValue, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	var tempOrder constant.TemporaryOrderInfo
	if err := tempOrder.Unmarshal([]byte(cacheValue)); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	if tempOrder.OrderNo != orderInfo.OrderNo || tempOrder.CheckoutToken == "" ||
		subtle.ConstantTimeCompare([]byte(tempOrder.CheckoutToken), []byte(req.CheckoutToken)) != 1 {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	return nil
}

// handleTemporaryOrder processes temporary order-related operations
func (l *QueryPurchaseOrderLogic) handleTemporaryOrder(orderInfo *order.Order) (string, error) {
	if orderInfo.UserId == 0 {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "guest account is not ready")
	}

	// Generate session token
	return l.generateSessionToken(orderInfo.UserId)
}

// generateSessionToken creates a session token and stores it in Redis
func (l *QueryPurchaseOrderLogic) generateSessionToken(userId int64) (string, error) {
	return IssuePurchaseSession(l.ctx, l.svcCtx, userId)
}

// IssuePurchaseSession creates the normal authenticated session issued after a
// guest purchase completes. Both this package's status endpoint and the V2
// capability-exchange endpoint use this helper so their token and Redis session
// semantics cannot drift.
func IssuePurchaseSession(ctx context.Context, svcCtx *svc.ServiceContext, userId int64) (string, error) {
	sessionId := uuidx.NewUUID().String()
	token, err := jwt.NewJwtToken(
		svcCtx.Config.JwtAuth.AccessSecret,
		time.Now().Unix(),
		svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userId),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Token generation error")
	}

	cacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err := svcCtx.Redis.Set(ctx, cacheKey, userId, time.Duration(svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Session storage error")
	}

	return token, nil
}

// fetchOrderDetails retrieves subscription and payment details
func (l *QueryPurchaseOrderLogic) fetchOrderDetails(orderInfo *order.Order) (types.Subscribe, types.PaymentMethod, error) {
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, orderInfo.SubscribeId)
	if err != nil {
		return types.Subscribe{}, types.PaymentMethod{}, wrapDatabaseError(err)
	}

	var subscribeInfo types.Subscribe
	tool.DeepCopy(&subscribeInfo, sub)

	payment, err := l.svcCtx.Store.Payment().FindOne(l.ctx, orderInfo.PaymentId)
	if err != nil {
		return types.Subscribe{}, types.PaymentMethod{}, wrapDatabaseError(err)
	}

	var paymentInfo types.PaymentMethod
	tool.DeepCopy(&paymentInfo, payment)

	return subscribeInfo, paymentInfo, nil
}
