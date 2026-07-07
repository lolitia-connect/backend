package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
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

func (l *QueryWithdrawalLogLogic) QueryWithdrawalLog(req *types.QueryWithdrawalLogListRequest) (resp *types.QueryWithdrawalLogListResponse, err error) {
	// todo: add your logic here and delete this line

	return
}
