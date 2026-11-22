package notify

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	paymentutil "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type EPayNotifyLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
	meta   EPayNotifyMeta
}

type EPayNotifyMeta struct {
	Method string
	Params map[string]string
	Values url.Values
}

// EPay notify
func NewEPayNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext, meta EPayNotifyMeta) *EPayNotifyLogic {
	if meta.Params == nil {
		meta.Params = make(map[string]string)
	}
	return &EPayNotifyLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
		meta:   meta,
	}
}

func (l *EPayNotifyLogic) EPayNotify(req *types.EPayNotifyRequest) error {
	store := l.svcCtx.Store
	// Find payment config
	data, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok || data == nil {
		l.Logger.Error("[EPayNotify] Payment not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}

	var config payment.EPayConfig
	if paymentutil.ParsePlatform(data.Platform) == paymentutil.CryptoSaaS {
		var crypto payment.CryptoSaaSConfig
		if err := crypto.Unmarshal([]byte(data.Config)); err != nil {
			return err
		}
		config.Pid, config.Url, config.Key, config.Type = crypto.AccountID, crypto.Endpoint, crypto.SecretKey, crypto.Type
	} else if err := json.Unmarshal([]byte(data.Config), &config); err != nil {
		l.Logger.Errorw("[EPayNotify] Unmarshal config failed", zap.Any("error", err.Error()))
		return err
	}

	gateway, err := epay.Open(epay.GatewayConfig{Version: config.Version, CheckoutMode: config.CheckoutMode, Endpoint: config.Url, Domain: data.Domain, MerchantID: config.Pid, Key: config.Key, PrivateKey: config.PrivateKey, PublicKey: config.PublicKey, PaymentType: config.Type})
	if err != nil {
		return err
	}
	values := l.meta.Values
	if values == nil {
		values = make(url.Values, len(l.meta.Params))
		for name, value := range l.meta.Params {
			values.Set(name, value)
		}
	}
	params, err := gateway.VerifyNotification(values)
	if err != nil {
		return rejectedNotification{err}
	}
	if req.OutTradeNo != params["out_trade_no"] || req.TradeNo != params["trade_no"] || req.TradeStatus != params["trade_status"] {
		return rejectedNotification{errors.New("payment notification differs from signed parameters")}
	}
	amount, err := epay.ParseMoney(params["money"])
	if err != nil {
		return err
	}
	queried, queryErr := gateway.QueryOrder(l.ctx, req.OutTradeNo, req.TradeNo)
	if queryErr != nil && !errors.Is(queryErr, epay.ErrQueryNotSupported) {
		return queryErr
	}
	if queryErr == nil {
		if queried == nil || !queried.Paid || queried.Refunded {
			return rejectedNotification{errors.New("gateway has not confirmed an unrefunded payment")}
		}
		if !queried.StatusOnly {
			queriedAmount, err := epay.ParseMoney(queried.Money)
			if err != nil || queriedAmount != amount || queried.OrderNo != req.OutTradeNo || queried.TradeNo != req.TradeNo || queried.MerchantID != config.Pid || queried.Type != params["type"] {
				return rejectedNotification{errors.New("gateway payment differs from signed notification")}
			}
		}
	}
	activate, err := settlePayment(l.ctx, store, data, req.OutTradeNo, req.TradeNo, amount, "CNY")
	if err != nil {
		return err
	}
	if activate {
		return enqueueActivation(l.ctx, l.svcCtx, req.OutTradeNo)
	}
	return nil
}
