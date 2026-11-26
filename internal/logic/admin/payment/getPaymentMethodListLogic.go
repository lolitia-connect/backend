package payment

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetPaymentMethodListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetPaymentMethodListLogic Get Payment Method List
func NewGetPaymentMethodListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPaymentMethodListLogic {
	return &GetPaymentMethodListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPaymentMethodListLogic) GetPaymentMethodList(req *types.GetPaymentMethodListRequest) (resp *types.GetPaymentMethodListResponse, err error) {
	total, list, err := l.svcCtx.Store.Payment().FindListByPage(l.ctx, req.Page, req.Size, &payment.Filter{
		Search: req.Search,
		Mark:   req.Platform,
		Enable: req.Enable,
	})
	if err != nil {
		l.Logger.Errorw("find payment method list error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method list error: %s", err.Error())
	}
	resp = &types.GetPaymentMethodListResponse{
		Total: total,
		List:  make([]types.PaymentMethodDetail, len(list)),
	}

	gateway := report.IsGatewayMode()
	for i, v := range list {
		config := make(map[string]interface{})
		_ = json.Unmarshal([]byte(v.Config), &config)
		notifyUrl := ""
		if paymentPlatform.ParsePlatform(v.Platform) != paymentPlatform.Balance {
			notifyUrl, _ = paymentPlatform.NotifyURL(v.Domain, l.svcCtx.Config.Site.Host, v.Platform, v.Token, gateway)
		}
		resp.List[i] = types.PaymentMethodDetail{
			Id:           v.Id,
			Name:         v.Name,
			Platform:     v.Platform,
			Icon:         v.Icon,
			Domain:       v.Domain,
			Config:       config,
			FeeMode:      v.FeeMode,
			FeePercent:   v.FeePercent,
			FeeAmount:    v.FeeAmount,
			Sort:         v.Sort,
			Enable:       *v.Enable,
			NotifyURL:    notifyUrl,
			Description:  v.Description,
			CurrencyUnit: v.CurrencyUnit,
			ExchangeRate: v.ExchangeRate,
			BillDesc:     v.BillDesc,
		}
	}
	return
}
