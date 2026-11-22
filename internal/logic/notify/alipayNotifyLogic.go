package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/payment/alipay"
	"go.uber.org/zap"
)

type AlipayNotifyLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Alipay notify
func NewAlipayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AlipayNotifyLogic {
	return &AlipayNotifyLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AlipayNotifyLogic) AlipayNotify(r *http.Request) error {
	store := l.svcCtx.Store
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return fmt.Errorf("payment config not found")
	}
	var config payment.AlipayF2FConfig
	if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
		l.Logger.Error("[AlipayNotify] Unmarshal config failed", zap.Any("error", err.Error()))
		return err
	}
	if err := r.ParseForm(); err != nil {
		l.Logger.Error("[AlipayNotify] Parse form failed", zap.Any("error", err.Error()))
		return err
	}
	client := alipay.NewClient(alipay.Config{
		AppId:       config.AppId,
		PrivateKey:  config.PrivateKey,
		PublicKey:   config.PublicKey,
		InvoiceName: config.InvoiceName,
		NotifyURL:   data.Domain + "/v1/payment/alipay/notify",
		Sandbox:     config.Sandbox,
	})
	notify, err := client.DecodeNotification(l.ctx, r.Form)
	if err != nil {
		l.Logger.Error("[AlipayNotify] Decode notification failed", zap.Any("error", err.Error()))
		return err
	}
	if notify.Status == alipay.Success {
		_, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
		if err != nil {
			l.Logger.Error("[AlipayNotify] Find order failed", zap.Any("error", err.Error()), zap.Any("orderNo", notify.OrderNo))
			return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
		}

		activate, err := settlePayment(l.ctx, store, data, notify.OrderNo, r.Form.Get("trade_no"), notify.Amount, "CNY")
		if err != nil {
			if !IsRejectedNotification(err) {
				l.Logger.Errorw("[AlipayNotify] Order settlement failed", "error", err)
			}
			return err
		}
		if activate {
			return enqueueActivation(l.ctx, l.svcCtx, notify.OrderNo)
		}
	} else {
		l.Logger.Debugw("[AlipayNotify] Payment not settled", "status", string(notify.Status))
	}
	return nil
}
