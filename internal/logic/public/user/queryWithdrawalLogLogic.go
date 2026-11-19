package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryWithdrawalLogLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryWithdrawalLogLogic Query Withdrawal Log
func NewQueryWithdrawalLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryWithdrawalLogLogic {
	return &QueryWithdrawalLogLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// QueryWithdrawalLog pages the current user's own withdrawal requests. The
// owner filter comes from the authenticated session rather than from the
// request, so a caller cannot read anyone else's withdrawals.
func (l *QueryWithdrawalLogLogic) QueryWithdrawalLog(req *types.QueryWithdrawalLogListRequest) (resp *types.QueryWithdrawalLogListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	page, size := normalizePage(req.Page, req.Size)

	userID := u.Id
	list, total, err := l.svcCtx.Store.User().QueryWithdrawalPageList(l.ctx, &user.WithdrawalFilter{UserId: &userID}, page, size)
	if err != nil {
		l.Logger.Errorf("Failed to query withdrawal log for user %d: %v", u.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to query withdrawal log for user %d: %v", u.Id, err)
	}

	resp = &types.QueryWithdrawalLogListResponse{
		Total: total,
		List:  make([]types.WithdrawalLog, 0, len(list)),
	}
	for _, item := range list {
		resp.List = append(resp.List, *withdrawalToType(item))
	}
	return resp, nil
}
