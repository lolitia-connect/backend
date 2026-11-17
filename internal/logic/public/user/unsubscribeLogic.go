package user

import (
	"context"
	"math/big"
	"sort"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/user"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UnsubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUnsubscribeLogic creates a new instance of UnsubscribeLogic for handling subscription cancellation
func NewUnsubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsubscribeLogic {
	return &UnsubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnsubscribeLogic) Unsubscribe(req *types.UnsubscribeRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || u == nil {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// find user subscription by ID
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("FindOneSubscribe failed", zap.Any("error", err.Error()), zap.Any("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe failed: %v", err.Error())
	}
	if userSub == nil || userSub.UserId != u.Id {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	if userSub.EntitlementSource != "" {
		return user.ErrProviderManaged
	}

	activate := []uint8{0, 1, 2}

	if !tool.Contains(activate, userSub.Status) {
		l.Logger.Errorw("Subscription status invalid for cancellation", zap.Any("userSubscribeId", userSub.Id), zap.Any("status", userSub.Status))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Subscription status invalid for cancellation")
	}

	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		walletInfo, err := store.Wallet().FindOneForUpdate(l.ctx, u.Id)
		if err != nil {
			return err
		}
		fresh, err := store.User().FindOneSubscribeForUpdate(l.ctx, req.Id)
		if err != nil {
			return err
		}
		if fresh == nil || fresh.UserId != u.Id {
			return xerr.NewErrCode(xerr.InvalidAccess)
		}
		transactionService := svc.ServiceContext{Store: store, Config: l.svcCtx.Config}
		remainingAmount, err := CalculateRemainingAmount(l.ctx, &transactionService, req.Id)
		if err != nil {
			return err
		}
		changed, err := store.User().CancelSubscribe(l.ctx, req.Id, u.Id)
		if err != nil {
			return err
		}
		if !changed {
			return errors.New("subscription was already canceled")
		}
		userSub = fresh
		if userSub.OrderId == 0 {
			return nil
		}

		// Query the original order information to determine refund strategy
		orderInfo, err := store.Order().FindOneDetails(l.ctx, userSub.OrderId)
		if err != nil {
			return err
		}
		if orderInfo == nil || orderInfo.UserId != u.Id {
			return xerr.NewErrCode(xerr.InvalidAccess)
		}
		remainingAmount = min(remainingAmount, orderInfo.RefundBasis())
		// Calculate refund distribution based on payment method and gift amount priority
		gift := min(orderInfo.RefundGiftBasis(), remainingAmount)
		balance := walletInfo.Balance + remainingAmount - gift

		// Create balance log entry only if there's an actual regular balance refund
		balanceRefundAmount := balance - walletInfo.Balance
		if balanceRefundAmount > 0 {
			balanceLog := log.Balance{
				OrderNo:   orderInfo.OrderNo,
				Amount:    balanceRefundAmount,
				Type:      log.BalanceTypeRefund, // Type 4 represents refund transaction
				Balance:   balance,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := balanceLog.Marshal()

			if err := store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeBalance.Uint8(),
				Date:     time.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				return err
			}
		}

		// Create gift amount log entry if there's a gift balance refund
		if gift > 0 {

			giftLog := log.Gift{
				SubscribeId: userSub.Id,
				OrderNo:     orderInfo.OrderNo,
				Type:        log.GiftTypeIncrease, // Type 1 represents gift amount increase
				Amount:      gift,
				Balance:     walletInfo.GiftAmount + gift,
				Remark:      "Unsubscribe refund",
			}
			content, _ := giftLog.Marshal()

			if err := store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     time.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				return err
			}
			// Update the wallet gift amount
			walletInfo.GiftAmount += gift
		}

		// Persist the refunded regular balance and gift amount
		walletInfo.Balance = balance
		if err := store.Wallet().UpdateBalanceFields(l.ctx, walletInfo); err != nil {
			return err
		}
		buyer, err := store.User().FindOne(l.ctx, u.Id)
		if err != nil {
			return err
		}
		if buyer == nil {
			return xerr.NewErrCode(xerr.InvalidAccess)
		}
		reversals, err := refundedCommissions(orderInfo, remainingAmount, buyer.RefererId)
		if err != nil {
			return err
		}
		referers := make([]int64, 0, len(reversals))
		for refererID := range reversals {
			referers = append(referers, refererID)
		}
		sort.Slice(referers, func(left, right int) bool { return referers[left] < referers[right] })
		for _, refererID := range referers {
			referer, err := store.Wallet().FindOneForUpdate(l.ctx, refererID)
			if err != nil {
				return err
			}
			reversed := reversals[refererID]
			referer.Commission -= reversed
			if err := store.Wallet().UpdateCommission(l.ctx, referer); err != nil {
				return err
			}
			content, err := (&log.Commission{Type: log.CommissionTypeRefund, Amount: -reversed, OrderNo: orderInfo.OrderNo, Balance: referer.Commission, Timestamp: time.Now().UnixMilli()}).Marshal()
			if err != nil {
				return err
			}
			if err := store.Log().Insert(l.ctx, &log.SystemLog{Type: log.TypeCommission.Uint8(), Date: time.Now().Format(time.DateOnly), ObjectID: refererID, Content: string(content)}); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		l.Logger.Errorw("Unsubscribe transaction failed", zap.Any("error", err.Error()), zap.Any("userId", u.Id), zap.Any("reqId", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unsubscribe transaction failed: %v", err.Error())
	}

	// Clear subscription cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("ClearSubscribeCache failed", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return err
}

func refundedCommissions(details *order.Details, refund, fallbackRefererID int64) (map[int64]int64, error) {
	reversals := make(map[int64]int64)
	basis := details.RefundBasis()
	if refund <= 0 || basis <= 0 {
		return reversals, nil
	}
	refund = min(refund, basis)
	earnings := make(map[int64]*big.Int)
	add := func(refererID, amount int64) {
		if refererID == 0 {
			refererID = fallbackRefererID
		}
		if refererID == 0 || amount <= 0 {
			return
		}
		if earnings[refererID] == nil {
			earnings[refererID] = new(big.Int)
		}
		earnings[refererID].Add(earnings[refererID], big.NewInt(amount))
	}
	add(details.CommissionRefererId, details.Commission)
	for _, renewal := range details.SubOrders {
		if renewal.IsPaidRenewal() {
			add(renewal.CommissionRefererId, renewal.Commission)
		}
	}
	for refererID, earned := range earnings {
		amount := new(big.Int).Mul(earned, big.NewInt(refund))
		amount.Quo(amount, big.NewInt(basis))
		if !amount.IsInt64() {
			return nil, errors.New("commission refund exceeds supported amount")
		}
		if amount.Sign() > 0 {
			reversals[refererID] = amount.Int64()
		}
	}
	return reversals, nil
}
