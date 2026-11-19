// Package orderLogic provides order processing logic for handling various types of orders
// including subscription purchases, renewals, traffic resets, and balance recharges.
package orderLogic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/perfect-panel/server/ent"
	entsubscriptionperiod "github.com/perfect-panel/server/ent/subscriptionperiod"
	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/model/entitlement"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/pkg/constant"
	"go.uber.org/zap"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
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
	"github.com/perfect-panel/server/pkg/authmethod"
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
	ErrUnfulfillable      = errors.New("order can no longer be fulfilled")
)

// ActivateOrderLogic handles the activation and processing of paid orders
type ActivateOrderLogic struct {
	svc        *svc.ServiceContext // Service context containing dependencies
	postCommit *[]func()
	committed  *ActivateOrderLogic
}

type activationStore struct {
	repository.Store
}

func (store activationStore) InTx(_ context.Context, action func(repository.Store) error) error {
	return action(store)
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

	var effects []func()
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		orderInfo, err := store.Order().FindOneByOrderNoForUpdate(ctx, payload.OrderNo)
		if err != nil {
			return err
		}
		if orderInfo == nil {
			return ErrInvalidOrderStatus
		}
		if orderInfo.Status == OrderStatusFinished || orderInfo.Status == OrderStatusClose {
			return nil
		}
		if orderInfo.Status != OrderStatusPaid {
			return ErrInvalidOrderStatus
		}
		if orderInfo.Method == entitlement.AppleIAPOrderMethod {
			return user.ErrProviderManaged
		}
		alreadyFulfilled := false
		if store.Ent() != nil && (orderInfo.Type == OrderTypeSubscribe || orderInfo.Type == OrderTypeRenewal) {
			alreadyFulfilled, err = store.Ent().SubscriptionPeriod.Query().
				Where(entsubscriptionperiod.ID(entitlement.LocalPeriodID(orderInfo.OrderNo))).
				Exist(ctx)
			if err != nil {
				return err
			}
		}
		if orderInfo.UserId != 0 {
			if _, err := store.Wallet().FindOneForUpdate(ctx, orderInfo.UserId); err != nil {
				return err
			}
		}
		transactionService := svc.ServiceContext{
			Store:       activationStore{Store: store},
			Ent:         store.Ent(),
			Config:      l.svc.Config,
			Redis:       l.svc.Redis,
			TelegramBot: l.svc.TelegramBot,
		}
		transactionLogic := &ActivateOrderLogic{svc: &transactionService, postCommit: &effects, committed: l}
		if !alreadyFulfilled {
			if err := transactionLogic.processOrderByType(ctx, orderInfo); err != nil {
				if errors.Is(err, ErrUnfulfillable) {
					return transactionLogic.refundUnfulfillable(ctx, orderInfo)
				}
				return err
			}
		}
		return transactionLogic.finalizeCouponAndOrder(ctx, orderInfo)
	})
	if err != nil {
		return err
	}
	for _, effect := range effects {
		effect()
	}
	return nil
}

func (l *ActivateOrderLogic) refundUnfulfillable(ctx context.Context, current *order.Order) error {
	if current.UserId == 0 || current.Amount < 0 || current.GiftAmount < 0 {
		return fmt.Errorf("order has no valid account or refund amount")
	}
	funds, err := l.svc.Store.Wallet().FindOneForUpdate(ctx, current.UserId)
	if err != nil {
		return err
	}
	now := time.Now()
	if current.Amount > 0 {
		funds.Balance += current.Amount
		content, err := (&log.Balance{Type: log.BalanceTypeRefund, Amount: current.Amount, OrderNo: current.OrderNo, Balance: funds.Balance, Timestamp: now.UnixMilli()}).Marshal()
		if err != nil {
			return err
		}
		if err := l.svc.Store.Log().Insert(ctx, &log.SystemLog{Type: log.TypeBalance.Uint8(), Date: now.Format(time.DateOnly), ObjectID: current.UserId, Content: string(content)}); err != nil {
			return err
		}
	}
	if current.GiftAmount > 0 {
		funds.GiftAmount += current.GiftAmount
		content, err := (&log.Gift{Type: log.GiftTypeIncrease, Amount: current.GiftAmount, OrderNo: current.OrderNo, Balance: funds.GiftAmount, Remark: "Unfulfillable order refund", Timestamp: now.UnixMilli()}).Marshal()
		if err != nil {
			return err
		}
		if err := l.svc.Store.Log().Insert(ctx, &log.SystemLog{Type: log.TypeGift.Uint8(), Date: now.Format(time.DateOnly), ObjectID: current.UserId, Content: string(content)}); err != nil {
			return err
		}
	}
	if err := l.svc.Store.Wallet().UpdateBalanceFields(ctx, funds); err != nil {
		return err
	}
	if current.CouponReserved && current.Coupon != "" {
		if err := l.svc.Store.Coupon().ReleaseUsage(ctx, current.Coupon); err != nil {
			return err
		}
	}
	if current.Type == OrderTypeSubscribe && current.SubscribeId > 0 {
		if err := l.svc.Store.Subscribe().RestoreInventory(ctx, current.SubscribeId); err != nil {
			return err
		}
	}
	closed, err := l.svc.Store.Order().ClosePaidOrder(ctx, current.OrderNo)
	if err != nil {
		return err
	}
	if !closed {
		return ErrInvalidOrderStatus
	}
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
func (l *ActivateOrderLogic) finalizeCouponAndOrder(ctx context.Context, orderInfo *order.Order) error {
	return l.svc.Store.InTx(ctx, func(store repository.Store) error {
		current, err := store.Order().FindOneByOrderNoForUpdate(ctx, orderInfo.OrderNo)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrInvalidOrderStatus
		}
		if current.Status == OrderStatusFinished {
			return nil
		}
		if current.Status != OrderStatusPaid {
			return ErrInvalidOrderStatus
		}
		if err := l.handleCommission(ctx, store, current); err != nil {
			return err
		}
		if current.Coupon != "" && !current.CouponReserved {
			if err := store.Coupon().UpdateCount(ctx, current.Coupon); err != nil {
				return err
			}
		}
		changed, err := store.Order().FinishOrder(ctx, current.OrderNo)
		if err != nil {
			return err
		}
		if !changed {
			return ErrInvalidOrderStatus
		}
		orderInfo.Status = OrderStatusFinished
		return nil
	})
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
	if err := l.recordLocalSubscriptionPeriod(ctx, orderInfo, userSub, false); err != nil {
		return err
	}

	// Trigger user group recalculation (runs in background)
	l.triggerUserGroupRecalculation(ctx, userInfo.Id)

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
	identity, err := l.svc.Store.User().FindUserAuthMethodByOpenID(ctx, tempOrder.AuthType, tempOrder.Identifier)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if identity != nil && identity.UserId > 0 {
		owner, err := l.svc.Store.User().FindOne(ctx, identity.UserId)
		if err != nil {
			return nil, err
		}
		if owner == nil || owner.DeletedAt != nil {
			return nil, fmt.Errorf("guest identity belongs to an unavailable account")
		}
		orderInfo.UserId = owner.Id
		if err := l.svc.Store.Order().Update(ctx, orderInfo); err != nil {
			return nil, err
		}
		return nil, ErrUnfulfillable
	}

	userInfo := &user.User{
		Password: tool.EncodePassWord(tempOrder.Password),
		Algo:     tool.PasswordAlgoArgon2id,
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
		)
		return nil, err
	}
	if tempOrder.OrderNo != orderNo {
		return nil, fmt.Errorf("temporary order identity does not match")
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
	if ent.IsNotFound(err) || (err == nil && sub == nil) {
		return nil, ErrUnfulfillable
	}
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

func (l *ActivateOrderLogic) handleCommission(ctx context.Context, store repository.Store, orderInfo *order.Order) error {
	if (orderInfo.Type != OrderTypeSubscribe && orderInfo.Type != OrderTypeRenewal) || orderInfo.Commission > 0 {
		return nil
	}
	buyer, err := store.User().FindOne(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}
	if buyer == nil || buyer.RefererId == 0 || buyer.RefererId == buyer.Id {
		return nil
	}
	referer, err := store.User().FindOne(ctx, buyer.RefererId)
	if err != nil {
		return err
	}
	if referer == nil {
		return nil
	}
	percentage := referer.ReferralPercentage
	onlyFirst := referer.OnlyFirstPurchase != nil && *referer.OnlyFirstPurchase
	if percentage == 0 {
		percentage = uint8(min(max(l.svc.Config.Invite.ReferralPercentage, 0), 100))
		onlyFirst = l.svc.Config.Invite.OnlyFirstPurchase
	}
	if percentage == 0 || orderInfo.Amount <= orderInfo.FeeAmount || orderInfo.FeeAmount < 0 {
		return nil
	}
	refererWallet, err := store.Wallet().FindOneForUpdate(ctx, referer.Id)
	if err != nil {
		return err
	}
	if onlyFirst {
		if !orderInfo.IsNew {
			return nil
		}
		commissioned, err := store.Order().HasCommissionedOrder(ctx, buyer.Id, orderInfo.OrderNo)
		if err != nil {
			return err
		}
		if commissioned {
			return nil
		}
	}
	amount := l.calculateCommission(orderInfo.Amount-orderInfo.FeeAmount, percentage)
	if amount <= 0 {
		return nil
	}
	refererWallet.Commission += amount
	if err := store.Wallet().UpdateCommission(ctx, refererWallet); err != nil {
		return err
	}
	if err := store.Order().SetCommission(ctx, orderInfo.Id, amount, referer.Id); err != nil {
		return err
	}
	commissionType := log.CommissionTypePurchase
	if orderInfo.Type == OrderTypeRenewal {
		commissionType = log.CommissionTypeRenewal
	}
	content, err := (&log.Commission{Type: commissionType, Amount: amount, Balance: refererWallet.Commission, OrderNo: orderInfo.OrderNo, Timestamp: time.Now().UnixMilli()}).Marshal()
	if err != nil {
		return err
	}
	return store.Log().Insert(ctx, &log.SystemLog{Type: log.TypeCommission.Uint8(), Date: time.Now().Format(time.DateOnly), ObjectID: referer.Id, Content: string(content)})
}

// calculateCommission computes the commission amount based on order price and referral percentage
func (l *ActivateOrderLogic) calculateCommission(price int64, percentage uint8) int64 {
	if price <= 0 {
		return 0
	}
	if percentage > 100 {
		percentage = 100
	}
	return price/100*int64(percentage) + price%100*int64(percentage)/100
}

// clearServerCache clears user list cache for all servers associated with the subscription
func (l *ActivateOrderLogic) clearServerCache(ctx context.Context, sub *subscribe.Subscribe) {
	if l.postCommit != nil {
		*l.postCommit = append(*l.postCommit, func() { l.committed.clearServerCache(ctx, sub) })
		return
	}
	if err := l.svc.Store.Subscribe().ClearCache(ctx, sub.Id); err != nil {
		zap.S().Error("[Order Queue] Clear subscribe cache failed", zap.Any("error", err.Error()))
	}
}

// triggerUserGroupRecalculation triggers user group recalculation after subscription changes
// This runs asynchronously in background to avoid blocking the main order processing flow
func (l *ActivateOrderLogic) triggerUserGroupRecalculation(ctx context.Context, userId int64) {
	if l.postCommit != nil {
		*l.postCommit = append(*l.postCommit, func() { l.committed.triggerUserGroupRecalculation(ctx, userId) })
		return
	}
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

	userSub, err := l.getUserSubscription(ctx, orderInfo)
	if err != nil {
		return err
	}

	sub, err := l.getSubscribeInfo(ctx, orderInfo.SubscribeId)
	if err != nil {
		return err
	}

	periodStart, trafficReset, err := l.updateSubscriptionForRenewal(ctx, userSub, sub, orderInfo)
	if err != nil {
		return err
	}
	if err := l.recordLocalSubscriptionPeriod(ctx, orderInfo, userSub, trafficReset, periodStart); err != nil {
		return err
	}

	// Clear cache
	l.clearServerCache(ctx, sub)

	// Send notifications
	l.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, telegram.RenewalNotify)

	return nil
}

// getUserSubscription retrieves user subscription by token
func (l *ActivateOrderLogic) getUserSubscription(ctx context.Context, orderInfo *order.Order) (*user.Subscribe, error) {
	current, err := findOrderSubscription(ctx, l.svc.Store.User(), orderInfo)
	if err != nil {
		return nil, err
	}
	current, err = l.svc.Store.User().FindOneSubscribeForUpdate(ctx, current.Id)
	if err != nil {
		return nil, err
	}
	if current == nil || current.UserId != orderInfo.UserId || current.SubscribeId != orderInfo.SubscribeId {
		return nil, fmt.Errorf("order subscription does not match its owner and plan")
	}
	if current.EntitlementSource != "" {
		return nil, user.ErrProviderManaged
	}
	if current.Status >= 4 {
		return nil, ErrUnfulfillable
	}
	return current, nil
}

type orderSubscriptionLookup interface {
	FindOneSubscribe(context.Context, int64) (*user.Subscribe, error)
	FindOneSubscribeByToken(context.Context, string) (*user.Subscribe, error)
}

func findOrderSubscription(ctx context.Context, lookup orderSubscriptionLookup, orderInfo *order.Order) (*user.Subscribe, error) {
	var userSub *user.Subscribe
	var err error
	if orderInfo.UserSubscribeId > 0 {
		userSub, err = lookup.FindOneSubscribe(ctx, orderInfo.UserSubscribeId)
	} else if orderInfo.SubscribeToken != "" {
		userSub, err = lookup.FindOneSubscribeByToken(ctx, orderInfo.SubscribeToken)
	} else {
		return nil, fmt.Errorf("order has no subscription reference")
	}
	if err != nil {
		zap.S().Error("Find user subscribe failed", zap.Any("error", err.Error()))
		return nil, err
	}
	if userSub == nil || userSub.UserId != orderInfo.UserId || userSub.SubscribeId != orderInfo.SubscribeId {
		return nil, fmt.Errorf("order subscription does not match its owner and plan")
	}
	return userSub, nil
}

// updateSubscriptionForRenewal updates subscription details for renewal including
// expiration time extension and traffic reset if configured
func (l *ActivateOrderLogic) updateSubscriptionForRenewal(ctx context.Context, userSub *user.Subscribe, sub *subscribe.Subscribe, orderInfo *order.Order) (time.Time, bool, error) {
	now := time.Now()
	periodStart := userSub.ExpireTime
	if periodStart.Before(now) {
		periodStart = now
	}
	if userSub.ExpireTime.Before(now) {
		userSub.ExpireTime = now
	}
	today := time.Now().Day()
	resetDay := userSub.ExpireTime.In(time.Local).Day()
	resetTraffic := (sub.RenewalReset != nil && *sub.RenewalReset) || today == resetDay

	// Reset traffic if enabled
	if resetTraffic {
		userSub.Download = 0
		userSub.Upload = 0
	}

	if userSub.FinishedAt != nil {
		if userSub.FinishedAt.Before(now) && today > resetDay {
			// reset user traffic if finished at is before now
			userSub.Download = 0
			userSub.Upload = 0
			resetTraffic = true
		}

		userSub.FinishedAt = nil
	}

	userSub.ExpireTime = tool.AddTime(sub.UnitTime, orderInfo.Quantity, userSub.ExpireTime)
	userSub.Status = 1
	userSub.TrafficUnlimited = sub.TrafficUnlimited
	// 续费时重置过期流量字段
	userSub.ExpiredDownload = 0
	userSub.ExpiredUpload = 0

	if err := l.svc.Store.User().RenewSubscribe(ctx, userSub, resetTraffic); err != nil {
		zap.S().Error("Update user subscribe failed", zap.Any("error", err.Error()))
		return time.Time{}, false, err
	}

	return periodStart, resetTraffic, nil
}

func (l *ActivateOrderLogic) recordLocalSubscriptionPeriod(ctx context.Context, orderInfo *order.Order, userSub *user.Subscribe, trafficReset bool, starts ...time.Time) error {
	if orderInfo.Type == OrderTypeResetTraffic {
		return nil
	}
	periodStart := userSub.StartTime
	if len(starts) > 0 {
		periodStart = starts[0]
	}
	_, err := l.svc.Ent.SubscriptionPeriod.Create().
		SetID(entitlement.LocalPeriodID(orderInfo.OrderNo)).
		SetEntitlementID(entitlement.LocalEntitlementID(userSub.Id)).
		SetTransactionKey(orderInfo.OrderNo).
		SetUserSubscribeID(userSub.Id).
		SetOrderID(orderInfo.Id).
		SetPlanID(orderInfo.SubscribeId).
		SetTrafficLimit(userSub.Traffic).
		SetStartAt(periodStart).
		SetEndAt(userSub.ExpireTime).
		SetTrafficReset(trafficReset).
		Save(ctx)
	return err
}

// ResetTraffic handles traffic quota reset for existing subscriptions
func (l *ActivateOrderLogic) ResetTraffic(ctx context.Context, orderInfo *order.Order) error {
	userInfo, err := l.getExistingUser(ctx, orderInfo.UserId)
	if err != nil {
		return err
	}

	userSub, err := l.getUserSubscription(ctx, orderInfo)
	if err != nil {
		return err
	}

	// Reset traffic
	userSub.Download = 0
	userSub.Upload = 0
	userSub.ExpiredDownload = 0
	userSub.ExpiredUpload = 0
	userSub.Status = 1

	if err := l.svc.Store.User().ResetSubscribeTraffic(ctx, userSub.Id); err != nil {
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
		return err
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
		walletInfo, err := store.Wallet().FindOneForUpdate(ctx, userInfo.Id)
		if err != nil {
			return err
		}
		walletInfo.Balance += orderInfo.Price
		if err = store.Wallet().UpdateBalanceFields(ctx, walletInfo); err != nil {
			return err
		}

		balanceLog := &log.Balance{
			Amount:    orderInfo.Price,
			Type:      log.BalanceTypeRecharge,
			OrderNo:   orderInfo.OrderNo,
			Balance:   walletInfo.Balance,
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

	// Send notifications
	l.sendRechargeNotifications(ctx, orderInfo, userInfo)

	return nil
}

// sendNotifications sends both user and admin notifications for order completion
func (l *ActivateOrderLogic) sendNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User, sub *subscribe.Subscribe, userSub *user.Subscribe, notifyType string) {
	if l.postCommit != nil {
		*l.postCommit = append(*l.postCommit, func() { l.committed.sendNotifications(ctx, orderInfo, userInfo, sub, userSub, notifyType) })
		return
	}
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		templateData := l.buildUserNotificationData(orderInfo, sub, userSub)
		if text, err := telegram.RenderMarkdownV2(notifyType, templateData); err == nil {
			l.sendUserNotifyWithTelegram(ctx, telegramId, text)
		}
	}

	// Send admin notification
	adminData := l.buildAdminNotificationData(orderInfo, sub, userInfo)
	if text, err := telegram.RenderMarkdownV2(telegram.AdminOrderNotify, adminData); err == nil {
		l.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// sendRechargeNotifications sends specific notifications for balance recharge orders
func (l *ActivateOrderLogic) sendRechargeNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User) {
	if l.postCommit != nil {
		*l.postCommit = append(*l.postCommit, func() { l.committed.sendRechargeNotifications(ctx, orderInfo, userInfo) })
		return
	}
	// Send user notification
	if telegramId, ok := findTelegram(userInfo); ok {
		balance := int64(0)
		if walletInfo, err := l.svc.Store.Wallet().FindOne(ctx, userInfo.Id); err == nil {
			balance = walletInfo.Balance
		}
		templateData := map[string]string{
			"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
			"PaymentMethod": orderInfo.Method,
			"Time":          orderInfo.CreatedAt.In(time.Local).Format("2006-01-02 15:04:05"),
			"Balance":       fmt.Sprintf("%.2f", float64(balance)/100),
		}
		if text, err := telegram.RenderMarkdownV2(telegram.RechargeNotify, templateData); err == nil {
			l.sendUserNotifyWithTelegram(ctx, telegramId, text)
		}
	}

	// Send admin notification
	adminData := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"UserEmail":     findEmail(userInfo),
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"SubscribeName": "余额充值",
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.In(time.Local).Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
	if text, err := telegram.RenderMarkdownV2(telegram.AdminOrderNotify, adminData); err == nil {
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
		data["ExpireTime"] = userSub.ExpireTime.In(time.Local).Format("2006-01-02 15:04:05")
		data["ResetTime"] = time.Now().Format("2006-01-02 15:04:05")
	}

	return data
}

// buildAdminNotificationData creates template data for admin notifications
func (l *ActivateOrderLogic) buildAdminNotificationData(orderInfo *order.Order, sub *subscribe.Subscribe, userInfo *user.User) map[string]string {
	subscribeName := sub.Name
	if orderInfo.Type == OrderTypeResetTraffic {
		subscribeName = "流量重置"
	}

	return map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"UserEmail":     findEmail(userInfo),
		"SubscribeName": subscribeName,
		"OrderAmount":   fmt.Sprintf("%.2f", float64(orderInfo.Price)/100),
		"OrderStatus":   "已支付",
		"OrderTime":     orderInfo.CreatedAt.In(time.Local).Format("2006-01-02 15:04:05"),
		"PaymentMethod": orderInfo.Method,
	}
}

// sendUserNotifyWithTelegram sends a notification message to a user via Telegram
func (l *ActivateOrderLogic) sendUserNotifyWithTelegram(ctx context.Context, chatId int64, text string) {
	if l.svc.TelegramBot == nil {
		return
	}
	if _, err := l.svc.TelegramBot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatId,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	}); err != nil {
		zap.S().Error("Send telegram user message failed", zap.Any("error", err.Error()))
	}
}

// sendAdminNotifyWithTelegram sends a notification message to all admin users via Telegram
func (l *ActivateOrderLogic) sendAdminNotifyWithTelegram(ctx context.Context, text string) {
	if l.svc.TelegramBot == nil {
		return
	}
	admins, err := l.svc.Store.User().QueryAdminUsers(ctx)
	if err != nil {
		zap.S().Error("Query admin users failed", zap.Any("error", err.Error()))
		return
	}

	for _, admin := range admins {
		if telegramId, ok := findTelegram(admin); ok {
			if _, err := l.svc.TelegramBot.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    telegramId,
				Text:      text,
				ParseMode: models.ParseModeMarkdown,
			}); err != nil {
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

// findEmail extracts the account email from the user's authentication methods.
// Identities live in auth methods rather than on the user row, so without this
// the administrator notification would render an empty account line.
func findEmail(u *user.User) string {
	for _, item := range u.AuthMethods {
		if item.AuthType == authmethod.Email {
			return item.AuthIdentifier
		}
	}
	return ""
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
			existingSubscribe, err = store.User().FindOneSubscribeForUpdate(ctx, existingSubscribe.Id)
			if err != nil {
				return err
			}
			if existingSubscribe == nil || existingSubscribe.UserId != userInfo.Id || existingSubscribe.SubscribeId != orderInfo.SubscribeId || existingSubscribe.Status >= 4 {
				return ErrUnfulfillable
			}
			if existingSubscribe.EntitlementSource != "" {
				return user.ErrProviderManaged
			}
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

			err = store.User().RedeemSubscribe(ctx, existingSubscribe, sub.Traffic > 0)
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
		l.clearServerCache(ctx, &subscribe.Subscribe{Id: existingSubscribe.SubscribeId})
	}

	// 8. 删除Redis临时数据
	if l.postCommit != nil {
		*l.postCommit = append(*l.postCommit, func() { l.svc.Redis.Del(ctx, cacheKey) })
	} else {
		l.svc.Redis.Del(ctx, cacheKey)
	}

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
