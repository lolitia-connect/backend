package coupon

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetCouponListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get coupon list
func NewGetCouponListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCouponListLogic {
	return &GetCouponListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetCouponListLogic) GetCouponList(req *types.GetCouponListRequest) (resp *types.GetCouponListResponse, err error) {
	resp = &types.GetCouponListResponse{}
	// get coupon list from db
	total, list, err := l.svcCtx.Store.Coupon().QueryCouponListByPage(l.ctx, int(req.Page), int(req.Size), req.Subscribe, req.Search)
	if err != nil {
		l.Logger.Errorw("[GetCouponList] Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get coupon list error: %v", err.Error())
	}
	resp.Total = total
	resp.List = make([]types.Coupon, 0)
	for _, coupon := range list {
		couponInfo := types.Coupon{}
		tool.DeepCopy(&couponInfo, coupon)
		couponInfo.Subscribe = tool.Int64SliceToStringSlice(tool.StringToInt64Slice(coupon.Subscribe))
		resp.List = append(resp.List, couponInfo)
	}
	return
}
