package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/alipayplus"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
)

type AlipayPlusNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAlipayPlusNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AlipayPlusNotifyLogic {
	return &AlipayPlusNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AlipayPlusNotifyLogic) AlipayPlusNotify(r *http.Request) error {
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return fmt.Errorf("payment config not found")
	}

	var config payment.AlipayPlusConfig
	if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
		l.Logger.Error("[AlipayPlusNotify] Unmarshal config failed", logger.Field("error", err.Error()))
		return err
	}

	client := alipayplus.NewClient(alipayplus.Config{
		ClientId:        config.ClientId,
		MerchantId:      config.MerchantId,
		PrivateKey:      config.PrivateKey,
		AlipayPublicKey: config.AlipayPublicKey,
		GatewayUrl:      config.GatewayUrl,
		Currency:        config.Currency,
		PaymentMethod:   config.PaymentMethod,
		InvoiceName:     config.InvoiceName,
	})

	notify, err := client.DecodeNotification(r)
	if err != nil {
		l.Logger.Error("[AlipayPlusNotify] Decode notification failed", logger.Field("error", err.Error()))
		return err
	}

	if notify.Status == alipayplus.Pending {
		l.Logger.Infow("[AlipayPlusNotify] Notify status pending", logger.Field("status", string(notify.Status)), logger.Field("orderNo", notify.OrderNo))
		return nil
	}

	if notify.Status != alipayplus.Success {
		l.Logger.Infow("[AlipayPlusNotify] Notify status is not success", logger.Field("status", string(notify.Status)), logger.Field("orderNo", notify.OrderNo))
		return nil
	}

	orderInfo, err := l.svcCtx.OrderModel.FindOneByOrderNo(l.ctx, notify.OrderNo)
	if err != nil {
		l.Logger.Error("[AlipayPlusNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
	}
	if orderInfo.Status != 1 {
		return nil
	}

	if err := l.svcCtx.OrderModel.UpdateOrderStatus(l.ctx, notify.OrderNo, 2); err != nil {
		l.Logger.Error("[AlipayPlusNotify] Update order status failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
		return err
	}

	payload := types.ForthwithActivateOrderPayload{
		OrderNo: notify.OrderNo,
	}
	bytes, err := json.Marshal(&payload)
	if err != nil {
		l.Logger.Error("[AlipayPlusNotify] Marshal payload failed", logger.Field("error", err.Error()))
		return err
	}

	task := asynq.NewTask(types.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
	taskInfo, err := l.svcCtx.Queue.EnqueueContext(l.ctx, task)
	if err != nil {
		l.Logger.Error("[AlipayPlusNotify] Enqueue task failed", logger.Field("error", err.Error()))
		return err
	}

	l.Logger.Info("[AlipayPlusNotify] Enqueue task success", logger.Field("taskInfo", taskInfo))
	return nil
}
