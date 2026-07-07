package portal

import (
	"context"
	"encoding/json"
	"github.com/perfect-panel/server/ent"

	"github.com/perfect-panel/server/pkg/tool"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type PrePurchaseOrderLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Pre Purchase Order
func NewPrePurchaseOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PrePurchaseOrderLogic {
	return &PrePurchaseOrderLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PrePurchaseOrderLogic) PrePurchaseOrder(req *types.PrePurchaseOrderRequest) (resp *types.PrePurchaseOrderResponse, err error) {
	// find subscribe plan
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribeId)
	if err != nil {
		l.Logger.Errorw("[PreCreateOrder] Database query error", zap.Any("error", err.Error()), zap.Any("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	var discount float64 = 1
	if sub.Discount != "" {
		var dis []types.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	amount := int64(float64(price) * discount)
	discountAmount := price - amount
	var coupon int64
	if req.Coupon != "" {
		couponInfo, err := l.svcCtx.Store.Coupon().FindOneByCode(l.ctx, req.Coupon)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotExist), "coupon not found")
			}
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if err := ensureCouponEnabled(couponInfo); err != nil {
			return nil, err
		}
		if couponInfo.Count != 0 && couponInfo.Count <= couponInfo.UsedCount {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used")
		}
		subs := tool.StringToInt64Slice(couponInfo.Subscribe)

		if len(subs) > 0 && !tool.Contains(subs, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}

		coupon = calculateCoupon(amount, couponInfo)
	}
	amount -= coupon
	var feeAmount int64
	if req.Payment != 0 {
		payment, err := l.svcCtx.Store.Payment().FindOne(l.ctx, req.Payment)
		if err != nil {
			l.Logger.Error("[PreCreateOrder] Database query error", zap.Any("error", err.Error()), zap.Any("payment", req.Payment))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
		}
		// Calculate the handling fee
		if amount > 0 {
			feeAmount = calculateFee(amount, payment)
		}
		amount += feeAmount
	}

	resp = &types.PrePurchaseOrderResponse{
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		FeeAmount:      feeAmount,
	}
	return
}
