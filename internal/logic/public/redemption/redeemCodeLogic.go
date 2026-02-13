package redemption

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type RedeemCodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Redeem code
func NewRedeemCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RedeemCodeLogic {
	return &RedeemCodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RedeemCodeLogic) RedeemCode(req *types.RedeemCodeRequest) (resp *types.RedeemCodeResponse, err error) {
	// Get user from context
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// 使用Redis分布式锁防止并发重复兑换
	lockKey := fmt.Sprintf("redemption_lock:%d:%s", u.Id, req.Code)
	lockSuccess, err := l.svcCtx.Redis.SetNX(l.ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil {
		l.Errorw("[RedeemCode] Acquire lock failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "system busy, please try again later")
	}
	if !lockSuccess {
		l.Errorw("[RedeemCode] Redemption in progress",
			logger.Field("user_id", u.Id),
			logger.Field("code", req.Code))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "redemption in progress, please wait")
	}
	defer l.svcCtx.Redis.Del(l.ctx, lockKey)

	// Find redemption code by code
	redemptionCode, err := l.svcCtx.RedemptionCodeModel.FindOneByCode(l.ctx, req.Code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			l.Errorw("[RedeemCode] Redemption code not found", logger.Field("code", req.Code))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code not found")
		}
		l.Errorw("[RedeemCode] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption code error: %v", err.Error())
	}

	// Check if redemption code is enabled
	if redemptionCode.Status != 1 {
		l.Errorw("[RedeemCode] Redemption code is disabled",
			logger.Field("code", req.Code),
			logger.Field("status", redemptionCode.Status))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code is disabled")
	}

	// Check if redemption code has remaining count
	if redemptionCode.TotalCount > 0 && redemptionCode.UsedCount >= redemptionCode.TotalCount {
		l.Errorw("[RedeemCode] Redemption code has been fully used",
			logger.Field("code", req.Code),
			logger.Field("total_count", redemptionCode.TotalCount),
			logger.Field("used_count", redemptionCode.UsedCount))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code has been fully used")
	}

	// Check if user has already redeemed this code
	userRecords, err := l.svcCtx.RedemptionRecordModel.FindByUserId(l.ctx, u.Id)
	if err != nil {
		l.Errorw("[RedeemCode] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption records error: %v", err.Error())
	}
	for _, record := range userRecords {
		if record.RedemptionCodeId == redemptionCode.Id {
			l.Errorw("[RedeemCode] User has already redeemed this code",
				logger.Field("user_id", u.Id),
				logger.Field("code", req.Code))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "you have already redeemed this code")
		}
	}

	// Find subscribe plan from redemption code
	subscribePlan, err := l.svcCtx.SubscribeModel.FindOne(l.ctx, redemptionCode.SubscribePlan)
	if err != nil {
		l.Errorw("[RedeemCode] Subscribe plan not found",
			logger.Field("subscribe_plan", redemptionCode.SubscribePlan),
			logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "subscribe plan not found")
	}

	// Check if subscribe plan is available
	if !*subscribePlan.Sell {
		l.Errorw("[RedeemCode] Subscribe plan is not available",
			logger.Field("subscribe_plan", redemptionCode.SubscribePlan))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe plan is not available")
	}

	// 检查配额限制（预检查，队列任务中会再次检查）
	if subscribePlan.Quota > 0 {
		var count int64
		err = l.svcCtx.DB.Model(&user.Subscribe{}).
			Where("user_id = ? AND subscribe_id = ?", u.Id, redemptionCode.SubscribePlan).
			Count(&count).Error
		if err != nil {
			l.Errorw("[RedeemCode] Check quota failed", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check quota failed")
		}
		if count >= subscribePlan.Quota {
			l.Errorw("[RedeemCode] Subscribe quota limit exceeded",
				logger.Field("user_id", u.Id),
				logger.Field("subscribe_id", redemptionCode.SubscribePlan),
				logger.Field("quota", subscribePlan.Quota),
				logger.Field("current_count", count))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "subscribe quota limit exceeded")
		}
	}

	// 判断是否首次购买
	isNew, err := l.svcCtx.OrderModel.IsUserEligibleForNewOrder(l.ctx, u.Id)
	if err != nil {
		l.Errorw("[RedeemCode] Check user order failed", logger.Field("error", err.Error()))
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
	err = l.svcCtx.OrderModel.Insert(l.ctx, orderInfo)
	if err != nil {
		l.Errorw("[RedeemCode] Create order failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create order failed")
	}

	// 缓存兑换码信息到Redis（供队列任务使用）
	cacheKey := fmt.Sprintf("redemption_order:%s", orderInfo.OrderNo)
	cacheData := map[string]interface{}{
		"redemption_code_id": redemptionCode.Id,
		"unit_time":          redemptionCode.UnitTime,
		"quantity":           redemptionCode.Quantity,
	}
	jsonData, _ := json.Marshal(cacheData)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, jsonData, 2*time.Hour).Err()
	if err != nil {
		l.Errorw("[RedeemCode] Cache redemption data failed", logger.Field("error", err.Error()))
		// 缓存失败，删除已创建的Order避免孤儿记录
		if delErr := l.svcCtx.OrderModel.Delete(l.ctx, orderInfo.Id); delErr != nil {
			l.Errorw("[RedeemCode] Delete order failed after cache error",
				logger.Field("order_id", orderInfo.Id),
				logger.Field("error", delErr.Error()))
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
		l.Errorw("[RedeemCode] Enqueue task failed", logger.Field("error", err.Error()))
		// 入队失败，删除Order和Redis缓存
		l.svcCtx.Redis.Del(l.ctx, cacheKey)
		if delErr := l.svcCtx.OrderModel.Delete(l.ctx, orderInfo.Id); delErr != nil {
			l.Errorw("[RedeemCode] Delete order failed after enqueue error",
				logger.Field("order_id", orderInfo.Id),
				logger.Field("error", delErr.Error()))
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "enqueue task failed")
	}

	l.Infow("[RedeemCode] Redemption order created successfully",
		logger.Field("order_no", orderInfo.OrderNo),
		logger.Field("user_id", u.Id),
	)

	return &types.RedeemCodeResponse{
		Message: "Redemption successful, processing...",
	}, nil
}