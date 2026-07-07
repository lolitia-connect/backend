package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type PreUnsubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPreUnsubscribeLogic Pre Unsubscribe
func NewPreUnsubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreUnsubscribeLogic {
	return &PreUnsubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PreUnsubscribeLogic) PreUnsubscribe(req *types.PreUnsubscribeRequest) (resp *types.PreUnsubscribeResponse, err error) {
	remainingAmount, err := CalculateRemainingAmount(l.ctx, l.svcCtx, req.Id)
	if err != nil {
		l.Logger.Errorw("[PreUnsubscribeLogic] Calculate Remaining Amount Error:", zap.Any("err", err.Error()))
		return nil, err
	}
	return &types.PreUnsubscribeResponse{
		DeductionAmount: remainingAmount,
	}, nil
}
