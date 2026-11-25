package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type QueryUserAffiliateLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query User Balance Log
func NewQueryUserAffiliateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserAffiliateLogic {
	return &QueryUserAffiliateLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserAffiliateLogic) QueryUserAffiliate() (resp *types.QueryUserAffiliateCountResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	total, err := l.svcCtx.Store.User().CountAffiliates(l.ctx, u.Id)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Affiliate failed: %v", err)
	}
	sum, err := l.svcCtx.Store.Log().SumUserCommission(l.ctx, u.Id)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query affiliate commission: %v", err)
	}

	return &types.QueryUserAffiliateCountResponse{
		Registers:       total,
		TotalCommission: sum,
	}, nil
}
