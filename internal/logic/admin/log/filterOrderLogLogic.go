package log

import (
	"context"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type FilterOrderLogLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Filter order creation audit log
func NewFilterOrderLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterOrderLogLogic {
	return &FilterOrderLogLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterOrderLogLogic) FilterOrderLog(req *types.FilterOrderLogRequest) (resp *types.FilterOrderLogResponse, err error) {
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:      req.Page,
		Size:      req.Size,
		Type:      log.TypeOrderCreated.Uint8(),
		ObjectID:  req.UserId,
		Data:      req.Date,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Search:    req.Search,
	})
	if err != nil {
		l.Logger.Errorf("[FilterOrderLog] failed to filter system log: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to filter system log: %v", err.Error())
	}

	list := make([]types.OrderLog, 0, len(data))
	for _, datum := range data {
		var content log.OrderCreated
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Logger.Errorf("[FilterOrderLog] failed to unmarshal content: %v", err.Error())
			continue
		}
		list = append(list, types.OrderLog{
			Id:               datum.Id,
			UserId:           datum.ObjectID,
			OrderNo:          content.OrderNo,
			OrderType:        content.OrderType,
			Quantity:         content.Quantity,
			Price:            content.Price,
			Amount:           content.Amount,
			GiftAmount:       content.GiftAmount,
			Discount:         content.Discount,
			CouponDiscount:   content.CouponDiscount,
			PaymentId:        content.PaymentID,
			Method:           content.Method,
			FeeAmount:        content.FeeAmount,
			SubscribeId:      content.SubscribeID,
			Source:           content.Source,
			Timestamp:        content.Timestamp,
			ClientIP:         content.ClientIP,
			UserAgent:        content.UserAgent,
			ActorID:          content.ActorID,
			IPCountryCode:    content.IPCountryCode,
			IPCountry:        content.IPCountry,
			IPRegion:         content.IPRegion,
			IPCity:           content.IPCity,
			IPASN:            content.IPASN,
			IPASOrganization: content.IPASOrganization,
		})
	}

	return &types.FilterOrderLogResponse{
		Total: total,
		List:  list,
	}, nil
}
