package order

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryOrderDetailLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get order
func NewQueryOrderDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryOrderDetailLogic {
	return &QueryOrderDetailLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryOrderDetailLogic) QueryOrderDetail(req *types.QueryOrderDetailRequest) (resp *types.OrderDetail, err error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneDetailsByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		l.Logger.Errorw("[QueryOrderDetail] Database query error", zap.Any("error", err.Error()), zap.Any("order_no", req.OrderNo))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find order error: %v", err.Error())
	}
	resp = &types.OrderDetail{}
	tool.DeepCopy(resp, orderInfo)
	// Prevent commission amount leakage
	resp.Commission = 0
	return
}
