package coupon

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/snowflake"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateCouponLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create coupon
func NewCreateCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCouponLogic {
	return &CreateCouponLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateCouponLogic) CreateCoupon(req *types.CreateCouponRequest) error {
	if req.Code == "" {
		rand.NewSource(time.Now().UnixNano())
		sid := snowflake.GetID()
		req.Code = random.KeyNew(4, 2) + "-" + random.StrToDashedString(random.EncodeBase36(sid))
	}
	couponInfo := &coupon.Coupon{}
	tool.DeepCopy(couponInfo, req)
	couponInfo.Subscribe = strings.Join(req.Subscribe, ",")
	enabled := true
	couponInfo.Enable = &enabled
	err := l.svcCtx.Store.Coupon().Insert(l.ctx, couponInfo)
	if err != nil {
		l.Logger.Errorw("[CreateCoupon] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create coupon error: %v", err.Error())
	}
	return nil
}
