package redemption

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/perfect-panel/server/ent"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

// unitTimeMapping maps lowercase API values to the format expected by tool.AddTime
var unitTimeMapping = map[string]string{
	"day":       "Day",
	"month":     "Month",
	"quarter":   "Quarter",
	"half_year": "HalfYear",
	"year":      "Year",
}

// normalizeUnitTime converts lowercase unit_time to the proper capitalized format
func normalizeUnitTime(unit string) string {
	if normalized, ok := unitTimeMapping[strings.ToLower(unit)]; ok {
		return normalized
	}
	return unit
}

type RedeemCodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Redeem code
func NewRedeemCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RedeemCodeLogic {
	return &RedeemCodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RedeemCodeLogic) RedeemCode(req *types.RedeemCodeRequest) (resp *types.RedeemCodeResponse, err error) {
	// Get user from context
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// 使用Redis分布式锁防止并发重复兑换
	lockKey := fmt.Sprintf("redemption_lock:%d:%s", u.Id, req.Code)
	lockSuccess, err := l.svcCtx.Redis.SetNX(l.ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Acquire lock failed", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "system busy, please try again later")
	}
	if !lockSuccess {
		l.Logger.Errorw("[RedeemCode] Redemption in progress",
			zap.Any("user_id", u.Id),
			zap.Any("code", req.Code))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "redemption in progress, please wait")
	}
	defer l.svcCtx.Redis.Del(l.ctx, lockKey)

	// Find redemption code by code
	redemptionCode, err := l.svcCtx.Store.RedemptionCode().FindOneByCode(l.ctx, req.Code)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Errorw("[RedeemCode] Redemption code not found", zap.Any("code", req.Code))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code not found")
		}
		l.Logger.Errorw("[RedeemCode] Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption code error: %v", err.Error())
	}

	// Check if redemption code is enabled
	if redemptionCode.Status != 1 {
		l.Logger.Errorw("[RedeemCode] Redemption code is disabled",
			zap.Any("code", req.Code),
			zap.Any("status", redemptionCode.Status))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code is disabled")
	}

	// Check if redemption code has remaining count
	if redemptionCode.TotalCount > 0 && redemptionCode.UsedCount >= redemptionCode.TotalCount {
		l.Logger.Errorw("[RedeemCode] Redemption code has been fully used",
			zap.Any("code", req.Code),
			zap.Any("total_count", redemptionCode.TotalCount),
			zap.Any("used_count", redemptionCode.UsedCount))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code has been fully used")
	}

	// Check if user has already redeemed this code
	userRecords, err := l.svcCtx.Store.RedemptionRecord().FindByUserId(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption records error: %v", err.Error())
	}
	for _, record := range userRecords {
		if record.RedemptionCodeId == redemptionCode.Id {
			l.Logger.Errorw("[RedeemCode] User has already redeemed this code",
				zap.Any("user_id", u.Id),
				zap.Any("code", req.Code))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "you have already redeemed this code")
		}
	}

	// Find subscribe plan from redemption code
	subscribePlan, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, redemptionCode.SubscribePlan)
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Subscribe plan not found",
			zap.Any("subscribe_plan", redemptionCode.SubscribePlan),
			zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "subscribe plan not found")
	}

	// Check if subscribe plan is available
	if !*subscribePlan.Sell {
		l.Logger.Errorw("[RedeemCode] Subscribe plan is not available",
			zap.Any("subscribe_plan", redemptionCode.SubscribePlan))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe plan is not available")
	}

	// 检查配额限制（预检查，队列任务中会再次检查）
	if subscribePlan.Quota > 0 {
		count, err := l.svcCtx.Store.User().CountUserSubscribesByUserAndSubscribe(l.ctx, u.Id, redemptionCode.SubscribePlan)
		if err != nil {
			l.Logger.Errorw("[RedeemCode] Check quota failed", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check quota failed")
		}
		if count >= subscribePlan.Quota {
			l.Logger.Errorw("[RedeemCode] Subscribe quota limit exceeded",
				zap.Any("user_id", u.Id),
				zap.Any("subscribe_id", redemptionCode.SubscribePlan),
				zap.Any("quota", subscribePlan.Quota),
				zap.Any("current_count", count))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "subscribe quota limit exceeded")
		}
	}

	// 判断是否首次购买
	isNew, err := l.svcCtx.Store.Order().IsUserEligibleForNewOrder(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Check user order failed", zap.Any("error", err.Error()))
		// 可以继续，默认为false
		isNew = false
	}

	// 创建Order记录
	orderInfo := &order.Order{
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           5, // 兑换类型
		Quantity:       redemptionCode.Quantity,
		Price:          0, // 兑换无价格
		Amount:         0, // 兑换无金额
		Discount:       0,
		GiftAmount:     0,
		Coupon:         "",
		CouponDiscount: 0,
		PaymentId:      0,
		Method:         "redemption",
		FeeAmount:      0,
		Commission:     0,
		Status:         2, // 直接设置为已支付
		SubscribeId:    redemptionCode.SubscribePlan,
		IsNew:          isNew,
	}

	// 保存Order到数据库
	err = l.svcCtx.Store.Order().Insert(l.ctx, orderInfo)
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Create order failed", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create order failed")
	}

	// 缓存兑换码信息到Redis（供队列任务使用）
	cacheKey := fmt.Sprintf("redemption_order:%s", orderInfo.OrderNo)
	cacheData := map[string]interface{}{
		"redemption_code_id": redemptionCode.Id,
		"unit_time":          normalizeUnitTime(redemptionCode.UnitTime),
		"quantity":           redemptionCode.Quantity,
	}
	jsonData, _ := json.Marshal(cacheData)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, jsonData, 2*time.Hour).Err()
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Cache redemption data failed", zap.Any("error", err.Error()))
		// 缓存失败，删除已创建的Order避免孤儿记录
		if delErr := l.svcCtx.Store.Order().Delete(l.ctx, orderInfo.Id); delErr != nil {
			l.Logger.Errorw("[RedeemCode] Delete order failed after cache error",
				zap.Any("order_id", orderInfo.Id),
				zap.Any("error", delErr.Error()))
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "cache redemption data failed")
	}

	// 触发队列任务
	payload := queue.ForthwithActivateOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	bytes, _ := json.Marshal(&payload)
	task := asynq.NewTask(queue.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
	_, err = l.svcCtx.Queue.EnqueueContext(l.ctx, task)
	if err != nil {
		l.Logger.Errorw("[RedeemCode] Enqueue task failed", zap.Any("error", err.Error()))
		// 入队失败，删除Order和Redis缓存
		l.svcCtx.Redis.Del(l.ctx, cacheKey)
		if delErr := l.svcCtx.Store.Order().Delete(l.ctx, orderInfo.Id); delErr != nil {
			l.Logger.Errorw("[RedeemCode] Delete order failed after enqueue error",
				zap.Any("order_id", orderInfo.Id),
				zap.Any("error", delErr.Error()))
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "enqueue task failed")
	}

	l.Logger.Infow("[RedeemCode] Redemption order created successfully",
		zap.Any("order_no", orderInfo.OrderNo),
		zap.Any("user_id", u.Id),
	)

	return &types.RedeemCodeResponse{
		Message: "Redemption successful, processing...",
	}, nil
}
