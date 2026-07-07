package order

import (
	"context"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateOrderLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create order
func NewCreateOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrderLogic {
	return &CreateOrderLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateOrderLogic) CreateOrder(req *types.CreateOrderRequest) error {
	store := l.svcCtx.Store
	paymentMethod, err := store.Payment().FindOne(l.ctx, req.PaymentId)
	if err != nil {
		l.Logger.Error("[CreateOrder] PaymentMethod Not Found", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "PaymentMethod not found: %v", err.Error())
	}

	err = store.Order().Insert(l.ctx, &order.Order{
		UserId:         req.UserId,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           req.Type,
		Quantity:       req.Quantity,
		Price:          req.Price,
		Amount:         req.Amount,
		Discount:       req.Discount,
		Coupon:         req.Coupon,
		CouponDiscount: req.CouponDiscount,
		PaymentId:      req.PaymentId,
		Method:         paymentMethod.Token,
		FeeAmount:      req.FeeAmount,
		TradeNo:        req.TradeNo,
		Status:         req.Status,
		SubscribeId:    req.SubscribeId,
	})
	if err != nil {
		l.Logger.Error("[CreateOrder] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Insert error: %v", err.Error())
	}
	return nil
}
