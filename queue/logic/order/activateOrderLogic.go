// Package orderLogic provides order processing logic for handling various types of orders
// including subscription purchases, renewals, traffic resets, and balance recharges.
package orderLogic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	entsystem "github.com/perfect-panel/server/ent/system"
	entusersubscribe "github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/pkg/constant"
	"go.uber.org/zap"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/redemption"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	queueTypes "github.com/perfect-panel/server/queue/types"
)

// Order type constants define the different types of orders that can be processed
const (
	OrderTypeSubscribe    = 1 // New subscription purchase
	OrderTypeRenewal      = 2 // Subscription renewal
	OrderTypeResetTraffic = 3 // Traffic quota reset
	OrderTypeRecharge     = 4 // Balance recharge
	OrderTypeRedemption   = 5 // Redemption code activation
)

// Order status constants define the lifecycle states of an order
const (
	OrderStatusPending  = 1 // Order created but not paid
	OrderStatusPaid     = 2 // Order paid and ready for processing
	OrderStatusClose    = 3 // Order closed/cancelled
	OrderStatusFailed   = 4 // Order processing failed
	OrderStatusFinished = 5 // Order successfully completed
)

// Predefined error variables for common error conditions
var (
	ErrInvalidOrderStatus = fmt.Errorf("invalid order status")
	ErrInvalidOrderType   = fmt.Errorf("invalid order type")
)

// ActivateOrderLogic handles the activation and processing of paid orders
type ActivateOrderLogic struct {
	svc *svc.ServiceContext // Service context containing dependencies
}

// NewActivateOrderLogic creates a new instance of ActivateOrderLogic
func NewActivateOrderLogic(svc *svc.ServiceContext) *ActivateOrderLogic {
	return &ActivateOrderLogic{
		svc: svc,
	}
}

// ProcessTask is the main entry point for processing order activation tasks.
// It handles the complete workflow of activating a paid order including validation,
// processing based on order type, and finalization.
func (l *ActivateOrderLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload, err := l.parsePayload(ctx, task.Payload())
	if err != nil {
		return err
	}

	orderInfo, err := l.validateAndGetOrder(ctx, payload.OrderNo)
	if err != nil {
		return err
	}
	if orderInfo == nil {
		return nil
	}

	if err = l.processOrderByType(ctx, orderInfo); err != nil {
		zap.S().Error("[ActivateOrderLogic] Process task failed", zap.Any("error", err.Error()))
		return err
	}

	l.finalizeCouponAndOrder(ctx, orderInfo)
	return nil
}

// parsePayload unMarshals the task payload into a structured format
func (l *ActivateOrderLogic) parsePayload(ctx context.Context, payload []byte) (*queueTypes.ForthwithActivateOrderPayload, error) {
	var p queueTypes.ForthwithActivateOrderPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		zap.S().Error("[ActivateOrderLogic] Unmarshal payload failed",
			zap.Any("error", err.Error()),
			zap.Any("payload", string(payload)),
		)
		return nil, err
	}
	return &p, nil
}

// validateAndGetOrder retrieves an order by order number and validates its status
// Returns error if order is not found or not in paid status
func (l *ActivateOrderLogic) validateAndGetOrder(ctx context.Context, orderNo string) (*order.Order, error) {
	orderInfo, err := l.svc.Store.Order().FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		zap.S().Error("Find order failed",
			zap.Any("error", err.Error()),
			zap.Any("order_no", orderNo),
		)
		return nil, err
	}

	if orderInfo.Status == OrderStatusFinished {
		zap.S().Info("Order already finished, skip processing",
			zap.Any("order_no", orderInfo.OrderNo),
		)
		return nil, nil
	}

	if orderInfo.Status != OrderStatusPaid {
		zap.S().Error("Order status error",
			zap.Any("order_no", orderInfo.OrderNo),
			zap.Any("status", orderInfo.Status),
		)
		return nil, ErrInvalidOrderStatus
	}

	return orderInfo, nil
}

// processOrderByType routes order processing based on the order type
func (l *ActivateOrderLogic) processOrderByType(ctx context.Context, orderInfo *order.Order) error {
	switch orderInfo.Type {
	case OrderTypeSubscribe:
		return l.NewPurchase(ctx, orderInfo)
	case OrderTypeRenewal:
		return l.Renewal(ctx, orderInfo)
	case OrderTypeResetTraffic:
		return l.ResetTraffic(ctx, orderInfo)
	case OrderTypeRecharge:
		return l.Recharge(ctx, orderInfo)
	case OrderTypeRedemption:
		return l.RedemptionActivate(ctx, orderInfo)
	default:
		zap.S().Error("Order type is invalid", zap.Any("type", orderInfo.Type))
		return ErrInvalidOrderType
	}
}

// finalizeCouponAndOrder handles post-processing tasks including coupon updates
// and order status finalization
func (l *ActivateOrderLogic) finalizeCouponAndOrder(ctx context.Context, orderInfo *order.Order) {
	// Update coupon if exists
	if orderInfo.Coupon != "" {
		if err := l.svc.Store.Coupon().UpdateCount(ctx, orderInfo.Coupon); err != nil {
			zap.S().Error("Update coupon status failed",
				zap.Any("error", err.Error()),
				zap.Any("coupon", orderInfo.Coupon),
			)
		}
	}

	// Update order status
	orderInfo.Status = OrderStatusFinished
	if err := l.svc.Store.Order().Update(ctx, orderInfo); err != nil {
		zap.S().Error("Update order status failed",
			zap.Any("error", err.Error()),
			zap.Any("order_no", orderInfo.OrderNo),
		)
	}
}

// NewPurchase handles new subscription purchase including user creation,
// subscription setup, commission processing, cache updates, and notifications
func (l *ActivateOrderLogic) NewPurchase(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getUserOrCreate(ctx, orderInfo)
	if err != nil {
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	userSub, err := l.createUserSubscription(ctx, orderInfo, sub)
	if err != nil {
		return err
	}

	// Trigger user group recalculation (runs in background)
	l.triggerUserGroupRecalculation(ctx, userInfo.Id)

	// Handle commission in separate goroutine to avoid blocking
	go l.handleCommission(context.Background(), userInfo, orderInfo)

	// Clear cache
	l.clearServerCache(ctx, sub)

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.PurchaseNotify)

	zap.S().Info("Insert user subscribe success")
	return nil
}

// getUserOrCreate retrieves an existing user or creates a new guest user based on order details
func (l *ActivateOrderLogic) getUserOrCreate(ctx context.Context, orderInfo *order.Order) (*user.User, error) {
	if orderInfo.UserId != 0 {
		return l.getExistingUser(ctx, orderInfo.UserId)
	}
	return l.createGuestUser(ctx, orderInfo)
}

// getExistingUser retrieves user information by user ID
func (l *ActivateOrderLogic) getExistingUser(ctx context.Context, userId int64) (*user.User, error) {
	userInfo, err := l.svc.Store.User().FindOne(ctx, userId)
	if err != nil {
		zap.S().Error("Find user failed",
			zap.Any("error", err.Error()),
			zap.Any("user_id", userId),
		)
		return nil, err
	}
	return userInfo, nil
}

// createGuestUser creates a new user account for guest orders using temporary order information
// stored in Redis cache
func (l *ActivateOrderLogic) createGuestUser(ctx context.Context, orderInfo *order.Order) (*user.User, error) {
	tempOrder, err := l.getTempOrderInfo(ctx, orderInfo.OrderNo)
	if err != nil {
		return nil, err
	}

	userInfo := &user.User{
		Password: tool.EncodePassWord(tempOrder.Password),
		Algo:     "default",
	}

	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		if err := store.User().Insert(ctx, userInfo); err != nil {
			return err
		}

		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		if err := store.User().Update(ctx, userInfo); err != nil {
			return err
		}

		if err := store.User().InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId:         userInfo.Id,
			AuthType:       tempOrder.AuthType,
			AuthIdentifier: tempOrder.Identifier,
		}); err != nil {
			return err
		}

		orderInfo.UserId = userInfo.Id
		return store.Order().Update(ctx, orderInfo)
	})

	if err != nil {
		zap.S().Error("Create user failed", zap.Any("error", err.Error()))
		return nil, err
	}

	// Handle referrer relationship
	l.handleReferrer(ctx, userInfo, tempOrder.InviteCode)

	zap.S().Info("Create guest user success",
		zap.Any("user_id", userInfo.Id),
		zap.Any("identifier", tempOrder.Identifier),
		zap.Any("auth_type", tempOrder.AuthType),
	)

	return userInfo, nil
}

// getTempOrderInfo retrieves temporary order information from Redis cache
func (l *ActivateOrderLogic) getTempOrderInfo(ctx context.Context, orderNo string) (*constant.TemporaryOrderInfo, error) {
	cacheKey := fmt.Sprintf(constant.TempOrderCacheKey, orderNo)
	data, err := l.svc.Redis.Get(ctx, cacheKey).Result()
	if err != nil {
		zap.S().Error("Get temp order cache failed",
			zap.Any("error", err.Error()),
			zap.Any("cache_key", cacheKey),
		)
		return nil, err
	}

	var tempOrder constant.TemporaryOrderInfo
	if err = tempOrder.Unmarshal([]byte(data)); err != nil {
		zap.S().Error("Unmarshal temp order cache failed",
			zap.Any("error", err.Error()),
			zap.Any("cache_key", cacheKey),
			zap.Any("data", data),
		)
		return nil, err
	}

	return &tempOrder, nil
}

// handleReferrer establishes referrer relationship if an invite code is provided
func (l *ActivateOrderLogic) handleReferrer(ctx context.Context, userInfo *user.User, inviteCode string) {
	if inviteCode == "" {
		return
	}

	referer, err := l.svc.Store.User().FindOneByReferCode(ctx, inviteCode)
	if err != nil {
		zap.S().Error("Find referer failed",
			zap.Any("error", err.Error()),
			zap.Any("refer_code", inviteCode),
		)
		return
	}

	userInfo.RefererId = referer.Id
	if err = l.svc.Store.User().Update(ctx, userInfo); err != nil {
		zap.S().Error("Update user referer failed",
			zap.Any("error", err.Error()),
			zap.Any("user_id", userInfo.Id),
		)
	}
}

// getSubscribeInfo retrieves subscription plan details by subscription ID
func (l *ActivateOrderLogic) getSubscribeInfo(ctx context.Context, subscribeId int64) (*subscribe.Subscribe, error) {
	sub, err := l.svc.Store.Subscribe().FindOne(ctx, subscribeId)
	if err != nil {
		zap.S().Error("Find subscribe failed",
			zap.Any("error", err.Error()),
			zap.Any("subscribe_id", subscribeId),
		)
		return nil, err
	}
	return sub, nil
}

// createUserSubscription creates a new user subscription record based on order and subscription plan details
func (l *ActivateOrderLogic) createUserSubscription(ctx context.Context, orderInfo *order.Order, sub *subscribe.Subscribe) (*user.Subscribe, error) {
	now := time.Now()
	userSub := &user.Subscribe{
		UserId:           orderInfo.UserId,
		OrderId:          orderInfo.Id,
		SubscribeId:      orderInfo.SubscribeId,
		StartTime:        now,
		ExpireTime:       tool.AddTime(sub.UnitTime, orderInfo.Quantity, now),
		Traffic:          sub.Traffic,
		TrafficUnlimited: sub.TrafficUnlimited,
		Download:         0,
		Upload:           0,
		ExpiredDownload:  0,
		ExpiredUpload:    0,
		Token:            uuidx.SubscribeToken(orderInfo.OrderNo),
		UUID:             uuid.New().String(),
		Status:           1,
		NodeGroupId:      sub.NodeGroupId, // Inherit node_group_id from subscription plan
	}

	// Check quota limit before creating subscription (final safeguard)
	if sub.Quota > 0 {
		count, err := l.svc.Ent.UserSubscribe.Query().
			Where(entusersubscribe.UserID(orderInfo.UserId), entusersubscribe.SubscribeID(orderInfo.SubscribeId)).
			Count(ctx)
		if err != nil {
			zap.S().Error("Count user subscribe failed", zap.Any("error", err.Error()))
			return nil, err
		}
		if int64(count) >= sub.Quota {
			zap.S().Infow("Subscribe quota limit exceeded",
				zap.Any("user_id", orderInfo.UserId),
				zap.Any("subscribe_id", orderInfo.SubscribeId),
				zap.Any("quota", sub.Quota),
				zap.Any("current_count", count),
			)
			return nil, fmt.Errorf("subscribe quota limit exceeded")
		}
	}

	if sub.Quota > 0 {
		count, err := l.svc.Store.User().CountUserSubscribesByUserAndSubscribe(ctx, orderInfo.UserId, orderInfo.SubscribeId)
		if err != nil {
			zap.S().Error("Count user subscribe failed", zap.Any("error", err.Error()))
			return nil, err
		}
		if count >= sub.Quota {
			zap.S().Info("Subscribe quota limit exceeded",
				zap.Any("user_id", orderInfo.UserId),
				zap.Any("subscribe_id", orderInfo.SubscribeId),
				zap.Any("quota", sub.Quota),
				zap.Any("current_count", count),
			)
			return nil, fmt.Errorf("subscribe quota limit exceeded")
		}
	}

	if err := l.svc.Store.User().InsertSubscribe(ctx, userSub); err != nil {
		zap.S().Error("Insert user subscribe failed", zap.Any("error", err.Error()))
		return nil, err
	}

	return userSub, nil
}

// handleCommission processes referral commission for the referrer if applicable.
// This runs asynchronously to avoid blocking the main order processing flow.
func (l *ActivateOrderLogic) handleCommission(ctx context.Context, userInfo *user.User, orderInfo *order.Order) {
	if !l.shouldProcessCommission(userInfo, orderInfo.IsNew) {
		return
	}

	referer, err := l.svc.Store.User().FindOne(ctx, userInfo.RefererId)
	if err != nil {
		zap.S().Error("Find referer failed",
			zap.Any("error", err.Error()),
			zap.Any("referer_id", userInfo.RefererId),
		)
		return
	}

	var referralPercentage uint8
	if referer.ReferralPercentage != 0 {
		referralPercentage = referer.ReferralPercentage
	} else {
		referralPercentage = uint8(l.svc.Config.Invite.ReferralPercentage)
	}

	// Order commission calculation： (Order Amount - Order Fee) * Referral Percentage
	amount := l.calculateCommission(orderInfo.Amount-orderInfo.FeeAmount, referralPercentage)

	// Use transaction for commission updates
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		referer.Commission += amount
		if err = store.User().Update(ctx, referer); err != nil {
			return err
		}

		var commissionType uint16
		switch orderInfo.Type {
		case OrderTypeSubscribe:
			commissionType = log.CommissionTypePurchase
		case OrderTypeRenewal:
			commissionType = log.CommissionTypeRenewal
		}

		commissionLog := &log.Commission{
			Type:      commissionType,
			Amount:    amount,
			OrderNo:   orderInfo.OrderNo,
			Timestamp: orderInfo.CreatedAt.UnixMilli(),
		}

		content, _ := commissionLog.Marshal()
		return store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeCommission.Uint8(),
			Date:     time.Now().Format("2006-01-02"),
			ObjectID: referer.Id,
			Content:  string(content),
		})
	})

	if err != nil {
		zap.S().Error("Update referer commission failed", zap.Any("error", err.Error()))
		return
	}

}

// shouldProcessCommission determines if commission should be processed based on
// referrer existence, commission settings, and order type
func (l *ActivateOrderLogic) shouldProcessCommission(userInfo *user.User, isFirstPurchase bool) bool {
	if userInfo == nil || userInfo.RefererId == 0 {
		return false
	}

	referer, err := l.svc.Store.User().FindOne(context.Background(), userInfo.RefererId)
	if err != nil {
		zap.S().Errorw("Find referer failed",
			zap.Any("error", err.Error()),
			zap.Any("referer_id", userInfo.RefererId))
		return false
	}
	if referer == nil {
		return false
	}

	// use referer's custom settings if set
	if referer.ReferralPercentage > 0 {
		if referer.OnlyFirstPurchase != nil && *referer.OnlyFirstPurchase && !isFirstPurchase {
			return false
		}
		return true
	}

	// use global settings
	if l.svc.Config.Invite.ReferralPercentage == 0 {
		return false
	}
	if l.svc.Config.Invite.OnlyFirstPurchase && !isFirstPurchase {
		return false
	}

	return true
}

// calculateCommission computes the commission amount based on order price and referral percentage
func (l *ActivateOrderLogic) calculateCommission(price int64, percentage uint8) int64 {
	return int64(float64(price) * (float64(percentage) / 100))
}

// clearServerCache clears user list cache for all servers associated with the subscription
func (l *ActivateOrderLogic) clearServerCache(ctx context.Context, sub *subscribe.Subscribe) {
	if err := l.svc.Store.Subscribe().ClearCache(ctx, sub.Id); err != nil {
		zap.S().Error("[Order Queue] Clear subscribe cache failed", zap.Any("error", err.Error()))
	}
}

// triggerUserGroupRecalculation triggers user group recalculation after subscription changes
// This runs asynchronously in background to avoid blocking the main order processing flow
func (l *ActivateOrderLogic) triggerUserGroupRecalculation(ctx context.Context, userId int64) {
	go func() {
		// Use a new context with timeout for group recalculation
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Check if group management is enabled
		groupEnabled, err := l.svc.Ent.System.Query().
			Where(entsystem.Category("group"), entsystem.Key("enabled")).
			Only(ctx)
		if err != nil || groupEnabled.Value != "true" && groupEnabled.Value != "1" {
			zap.S().Debugf("[Group Trigger] Group management not enabled, skipping recalculation")
			return
		}

		// Get the configured grouping mode
		groupMode, err := l.svc.Ent.System.Query().
			Where(entsystem.Category("group"), entsystem.Key("mode")).
			Only(ctx)
		if err != nil {
			zap.S().Errorw("[Group Trigger] Failed to get group mode", zap.Any("error", err.Error()))
			return
		}

		// Validate group mode
		if groupMode.Value != "average" && groupMode.Value != "subscribe" && groupMode.Value != "traffic" {
			zap.S().Debugf("[Group Trigger] Invalid group mode (current: %s), skipping", groupMode.Value)
			return
		}

		// Trigger group recalculation with the configured mode
		logic := group.NewRecalculateGroupLogic(ctx, l.svc)
		req := &types.RecalculateGroupRequest{
			Mode: groupMode.Value,
		}

		if err := logic.RecalculateGroup(req); err != nil {
			zap.S().Errorw("[Group Trigger] Failed to recalculate user group",
				zap.Any("user_id", userId),
				zap.Any("error", err.Error()),
			)
			return
		}

		zap.S().Infow("[Group Trigger] Successfully recalculated user group",
			zap.Any("user_id", userId),
			zap.Any("mode", groupMode.Value),
		)
	}()
}

// Renewal handles subscription renewal including subscription extension,
// traffic reset (if configured), commission processing, and notifications
func (l *ActivateOrderLogic) Renewal(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	userSub, err := l.getUserSubscription(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	if err = l.updateSubscriptionForRenewal(ctx, userSub, sub, orderInfo); err != nil {
		return err
	}

	// Clear cache
	l.clearServerCache(ctx, sub)

	// Handle commission
	go l.handleCommission(context.Background(), userInfo, orderInfo)

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.RenewalNotify)

	return nil
}

// getUserSubscription retrieves user subscription by token
func (l *ActivateOrderLogic) getUserSubscription(ctx context.Context, token string) (*user.Subscribe, error) {
	userSub, err := l.svc.Store.User().FindOneSubscribeByToken(ctx, token)
	if err != nil {
		zap.S().Error("Find user subscribe failed", zap.Any("error", err.Error()))
		return nil, err
	}
	return userSub, nil
}

// updateSubscriptionForRenewal updates subscription details for renewal including
// expiration time extension and traffic reset if configured
func (l *ActivateOrderLogic) updateSubscriptionForRenewal(ctx context.Context, userSub *user.Subscribe, sub *subscribe.Subscribe, orderInfo *order.Order) error {
	now := time.Now()
	if userSub.ExpireTime.Before(now) {
		userSub.ExpireTime = now
	}
	today := time.Now().Day()
	resetDay := userSub.ExpireTime.Day()

	// Reset traffic if enabled
	if (sub.RenewalReset != nil && *sub.RenewalReset) || today == resetDay {
		userSub.Download = 0
		userSub.Upload = 0
	}

	if userSub.FinishedAt != nil {
		if userSub.FinishedAt.Before(now) && today > resetDay {
			// reset user traffic if finished at is before now
			userSub.Download = 0
			userSub.Upload = 0
		}

		userSub.FinishedAt = nil
	}

	userSub.ExpireTime = tool.AddTime(sub.UnitTime, orderInfo.Quantity, userSub.ExpireTime)
	userSub.Status = 1
	userSub.TrafficUnlimited = sub.TrafficUnlimited
	// 续费时重置过期流量字段
	userSub.ExpiredDownload = 0
	userSub.ExpiredUpload = 0

	if err := l.svc.Store.User().UpdateSubscribe(ctx, userSub); err != nil {
		zap.S().Error("Update user subscribe failed", zap.Any("error", err.Error()))
		return err
	}

	return nil
}

// ResetTraffic handles traffic quota reset for existing subscriptions
func (l *ActivateOrderLogic) ResetTraffic(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	userSub, err := l.getUserSubscription(ctx, orderInfo.SubscribeToken)
	if err != nil {
		return err
	}

	// Reset traffic
	userSub.Download = 0
	userSub.Upload = 0
	userSub.ExpiredDownload = 0
	userSub.ExpiredUpload = 0
	userSub.Status = 1

	if err := l.svc.Store.User().UpdateSubscribe(ctx, userSub); err != nil {
		zap.S().Error("Update user subscribe failed", zap.Any("error", err.Error()))
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, userSub.SubscribeId)
	if err != nil {
		return err
	}

	// Clear cache
	l.clearServerCache(ctx, sub)

	// insert reset traffic log
	resetLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypePaid,
		UserId:    userInfo.Id,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: time.Now().UnixMilli(),
	}

	content, _ := resetLog.Marshal()
	if err = l.svc.Store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Date:     time.Now().Format(time.DateOnly),
		ObjectID: userSub.Id,
		Content:  string(content),
	}); err != nil {
		zap.S().Error("[Order Queue]Insert reset subscribe log failed", zap.Any("error", err.Error()))
	}

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.ResetTrafficNotify)

	return nil
}

// Recharge handles balance recharge orders including balance updates,
// transaction logging, and notifications
func (l *ActivateOrderLogic) Recharge(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	// Update balance in transaction
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		userInfo.Balance += orderInfo.Price
		if err = store.User().Update(ctx, userInfo); err != nil {
			return err
		}

		balanceLog := &log.Balance{
			Amount:    orderInfo.Price,
			Type:      log.BalanceTypeRecharge,
			OrderNo:   orderInfo.OrderNo,
			Balance:   userInfo.Balance,
			Timestamp: time.Now().UnixMilli(),
		}
		content, _ := balanceLog.Marshal()

		return store.Log().Insert(ctx, &log.SystemLog{
			Type:     log.TypeBalance.Uint8(),
			Date:     time.Now().Format("2006-01-02"),
			ObjectID: userInfo.Id,
			Content:  string(content),
		})
	})

	if err != nil {
		zap.S().Error("[Recharge] Database transaction failed", zap.Any("error", err.Error()))
		return err
	}

	// clear user cache
	if err = l.svc.Store.User().Update(ctx, userInfo); err != nil {
		zap.S().Error("[Recharge] Update user cache failed", zap.Any("error", err.Error()))
		return err
	}

	// Send notifications
	l.sendRechargeNotifications(ctx, orderInfo, userInfo)

	return nil
}

// sendNotifications sends both user and admin notifications for order completion
func (l *ActivateOrderLogic) sendNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User, sub *subscribe.Subscribe, userSub *user.Subscribe, notifyType string) {
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		templateData := l.buildUserNotificationData(orderInfo, sub, userSub)
		if text, err := tool.RenderTemplateToString(notifyType, templateData); err == nil {
			l.sendUserNotifyWithTelegram(telegramId, text)
		}
	}

	// Send admin notification
	adminData := l.buildAdminNotificationData(orderInfo, sub)
	if text, err := tool.RenderTemplateToString(telegram.AdminOrderNotify, adminData); err == nil {
		l.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// sendRechargeNotifications sends specific notifications for balance recharge orders
func (l *ActivateOrderLogic) sendRechargeNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User) {
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		templateData := map[string]string{
			"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
			"PaymentMethod": orderInfo.Method,
			"Time":          orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
			"Balance":       fmt.Sprintf("%.2f", float64(userInfo.Balance)/100),
		}
		if text, err := tool.RenderTemplateToString(telegram.RechargeNotify, templateData); err == nil {
			l.sendUserNotifyWithTelegram(telegramId, text)
		}
	}

	// Send admin notification
	adminData := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"SubscribeName": "余额充值",
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
	if text, err := tool.RenderTemplateToString(telegram.AdminOrderNotify, adminData); err == nil {
		l.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// buildUserNotificationData creates template data for user notifications
func (l *ActivateOrderLogic) buildUserNotificationData(orderInfo *order.Order, sub *subscribe.Subscribe, userSub *user.Subscribe) map[string]string {
	data := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"SubscribeName": sub.Name,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
	}

	if userSub != nil {
		data["ExpireTime"] = userSub.ExpireTime.Format("2006-01-02 15:04:05")
		data["ResetTime"] = time.Now().Format("2006-01-02 15:04:05")
	}

	return data
}

// buildAdminNotificationData creates template data for admin notifications
func (l *ActivateOrderLogic) buildAdminNotificationData(orderInfo *order.Order, sub *subscribe.Subscribe) map[string]string {
	subscribeName := sub.Name
	if orderInfo.Type == OrderTypeResetTraffic {
		subscribeName = "流量重置"
	}

	return map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"SubscribeName": subscribeName,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
}

// sendUserNotifyWithTelegram sends a notification message to a user via Telegram
func (l *ActivateOrderLogic) sendUserNotifyWithTelegram(chatId int64, text string) {
	msg := tgbotapi.NewMessage(chatId, text)
	msg.ParseMode = "markdown"
	if _, err := l.svc.TelegramBot.Send(msg); err != nil {
		zap.S().Error("Send telegram user message failed", zap.Any("error", err.Error()))
	}
}

// sendAdminNotifyWithTelegram sends a notification message to all admin users via Telegram
func (l *ActivateOrderLogic) sendAdminNotifyWithTelegram(ctx context.Context, text string) {
	admins, err := l.svc.Store.User().QueryAdminUsers(ctx)
	if err != nil {
		zap.S().Error("Query admin users failed", zap.Any("error", err.Error()))
		return
	}

	for _, admin := range admins {
		if telegramId, ok := findTelegram(admin); ok {
			msg := tgbotapi.NewMessage(telegramId, text)
			msg.ParseMode = "markdown"
			if _, err := l.svc.TelegramBot.Send(msg); err != nil {
				zap.S().Error("Send telegram admin message failed", zap.Any("error", err.Error()))
			}
		}
	}
}

// findTelegram extracts Telegram chat ID from user authentication methods.
// Returns the chat ID and a boolean indicating if Telegram auth was found.
func findTelegram(u *user.User) (int64, bool) {
	for _, item := range u.AuthMethods {
		if item.AuthType == "telegram" {
			if telegramId, err := strconv.ParseInt(item.AuthIdentifier, 10, 64); err == nil {
				return telegramId, true
			}
		}
	}
	return 0, false
}

// RedemptionActivate handles redemption code activation including subscription creation,
// redemption record creation, used count update, cache clearing, and notifications
func (l *ActivateOrderLogic) RedemptionActivate(ctx context.Context, orderInfo *order.Order) error {
	// 1. 获取用户信息
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	// 2. 获取套餐信息
	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	// 3. 从Redis获取兑换码信息
	cacheKey := fmt.Sprintf("redemption_order:%s", orderInfo.OrderNo)
	data, err := l.svc.Redis.Get(ctx, cacheKey).Result()
	if err != nil {
		zap.S().Error("Get redemption cache failed",
			zap.Any("error", err.Error()),
			zap.Any("cache_key", cacheKey),
		)
		return err
	}

	var redemptionData struct {
		RedemptionCodeId int64  `json:"redemption_code_id"`
		UnitTime         string `json:"unit_time"`
		Quantity         int64  `json:"quantity"`
	}
	if err = json.Unmarshal([]byte(data), &redemptionData); err != nil {
		zap.S().Error("Unmarshal redemption cache failed", zap.Any("error", err.Error()))
		return err
	}

	// 4. 幂等性检查：查询是否已有兑换记录
	existingRecords, err := l.svc.Store.RedemptionRecord().FindByUserId(ctx, userInfo.Id)
	if err == nil {
		for _, record := range existingRecords {
			if record.RedemptionCodeId == redemptionData.RedemptionCodeId {
				zap.S().Info("Redemption already processed, skip",
					zap.Any("order_no", orderInfo.OrderNo),
					zap.Any("user_id", userInfo.Id),
					zap.Any("redemption_code_id", redemptionData.RedemptionCodeId),
				)
				// 已处理过，直接返回成功（幂等性保护）
				return nil
			}
		}
	}

	// 5. 查找用户现有订阅
	var existingSubscribe *user.Subscribe
	userSubscribes, err := l.svc.Store.User().QueryUserSubscribe(ctx, userInfo.Id, 0, 1)
	if err == nil {
		for _, us := range userSubscribes {
			if us.SubscribeId == orderInfo.SubscribeId {
				existingSubscribe = &user.Subscribe{
					Id:          us.Id,
					UserId:      us.UserId,
					SubscribeId: us.SubscribeId,
					ExpireTime:  us.ExpireTime,
					Status:      us.Status,
					Traffic:     us.Traffic,
					Download:    us.Download,
					Upload:      us.Upload,
					NodeGroupId: us.NodeGroupId,
					Token:       us.Token,
					UUID:        us.UUID,
				}
				break
			}
		}
	}

	now := time.Now()

	// 6. 使用事务保护核心操作
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// 6.1 创建或更新订阅
		if existingSubscribe != nil {
			// 续期现有订阅
			var newExpireTime time.Time
			if existingSubscribe.ExpireTime.After(now) {
				newExpireTime = existingSubscribe.ExpireTime
			} else {
				newExpireTime = now
			}

			// 计算新的过期时间
			newExpireTime = tool.AddTime(redemptionData.UnitTime, redemptionData.Quantity, newExpireTime)

			// 更新订阅
			existingSubscribe.OrderId = orderInfo.Id // 设置OrderId用于追溯
			existingSubscribe.ExpireTime = newExpireTime
			existingSubscribe.Status = 1
			existingSubscribe.TrafficUnlimited = sub.TrafficUnlimited

			// 重置流量（如果套餐有流量限制）
			if sub.Traffic > 0 {
				existingSubscribe.Traffic = sub.Traffic
				existingSubscribe.Download = 0
				existingSubscribe.Upload = 0
			}

			err = store.User().UpdateSubscribe(ctx, existingSubscribe)
			if err != nil {
				zap.S().Error("Update subscribe failed", zap.Any("error", err.Error()))
				return err
			}

			zap.S().Info("Extended existing subscription",
				zap.Any("subscribe_id", existingSubscribe.Id),
				zap.Any("new_expire_time", newExpireTime),
			)
		} else {
			// 检查配额限制
			if sub.Quota > 0 {
				count, err := store.User().CountUserSubscribesByUserAndSubscribe(ctx, userInfo.Id, orderInfo.SubscribeId)
				if err != nil {
					zap.S().Error("Count user subscribe failed", zap.Any("error", err.Error()))
					return err
				}
				if int64(count) >= sub.Quota {
					zap.S().Infow("Subscribe quota limit exceeded",
						zap.Any("user_id", userInfo.Id),
						zap.Any("subscribe_id", orderInfo.SubscribeId),
						zap.Any("quota", sub.Quota),
						zap.Any("current_count", count),
					)
					return fmt.Errorf("subscribe quota limit exceeded")
				}
			}

			// 创建新订阅
			expireTime := tool.AddTime(redemptionData.UnitTime, redemptionData.Quantity, now)
			traffic := int64(0)
			if sub.Traffic > 0 {
				traffic = sub.Traffic
			}

			newSubscribe := &user.Subscribe{
				UserId:           userInfo.Id,
				OrderId:          orderInfo.Id,
				SubscribeId:      orderInfo.SubscribeId,
				StartTime:        now,
				ExpireTime:       expireTime,
				FinishedAt:       nil,
				Traffic:          traffic,
				TrafficUnlimited: sub.TrafficUnlimited,
				Download:         0,
				Upload:           0,
				Token:            uuidx.SubscribeToken(orderInfo.OrderNo),
				UUID:             uuid.New().String(),
				Status:           1,
				NodeGroupId:      sub.NodeGroupId, // Inherit node_group_id from subscription plan
			}

			err = store.User().InsertSubscribe(ctx, newSubscribe)
			if err != nil {
				zap.S().Error("Insert subscribe failed", zap.Any("error", err.Error()))
				return err
			}

			zap.S().Info("Created new subscription",
				zap.Any("subscribe_id", newSubscribe.Id),
				zap.Any("expire_time", expireTime),
			)
		}

		// 6.2 更新兑换码使用次数
		err = store.RedemptionCode().IncrementUsedCount(ctx, redemptionData.RedemptionCodeId)
		if err != nil {
			zap.S().Error("Increment used count failed", zap.Any("error", err.Error()))
			return err
		}

		// 6.3 创建兑换记录
		redemptionRecord := &redemption.RedemptionRecord{
			RedemptionCodeId: redemptionData.RedemptionCodeId,
			UserId:           userInfo.Id,
			SubscribeId:      orderInfo.SubscribeId,
			UnitTime:         redemptionData.UnitTime,
			Quantity:         redemptionData.Quantity,
			RedeemedAt:       now,
			CreatedAt:        now,
		}

		err = store.RedemptionRecord().Insert(ctx, redemptionRecord)
		if err != nil {
			zap.S().Error("Insert redemption record failed", zap.Any("error", err.Error()))
			return err
		}

		return nil
	})

	if err != nil {
		zap.S().Error("Redemption transaction failed", zap.Any("error", err.Error()))
		return err
	}

	// Trigger user group recalculation (runs in background)
	l.triggerUserGroupRecalculation(ctx, userInfo.Id)

	// 7. 清理缓存（关键步骤：让节点获取最新订阅）
	l.clearServerCache(ctx, sub)

	// 7.1 清理用户订阅缓存（确保用户端显示最新信息）
	if existingSubscribe != nil {
		err = l.svc.Store.Subscribe().ClearCache(ctx, existingSubscribe.SubscribeId)
		if err != nil {
			zap.S().Error("Clear user subscribe cache failed",
				zap.Any("error", err.Error()),
				zap.Any("subscribe_id", existingSubscribe.Id),
				zap.Any("user_id", userInfo.Id),
			)
		}
	}

	// 8. 删除Redis临时数据
	l.svc.Redis.Del(ctx, cacheKey)

	// 9. 发送通知（可选）
	// 可以复用现有的通知模板或创建新的兑换通知模板
	// l.sendNotifications(ctx, orderInfo, userInfo, sub, existingSubscribe, telegram.RedemptionNotify)

	zap.S().Info("Redemption activation success",
		zap.Any("order_no", orderInfo.OrderNo),
		zap.Any("user_id", userInfo.Id),
		zap.Any("subscribe_id", orderInfo.SubscribeId),
	)

	return nil
}
