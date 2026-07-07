package redemption

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetRedemptionRecordListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get redemption record list
func NewGetRedemptionRecordListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRedemptionRecordListLogic {
	return &GetRedemptionRecordListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRedemptionRecordListLogic) GetRedemptionRecordList(req *types.GetRedemptionRecordListRequest) (resp *types.GetRedemptionRecordListResponse, err error) {
	total, list, err := l.svcCtx.Store.RedemptionRecord().QueryRedemptionRecordListByPage(
		l.ctx,
		int(req.Page),
		int(req.Size),
		req.UserId,
		req.CodeId,
	)
	if err != nil {
		l.Logger.Errorw("[GetRedemptionRecordList] Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get redemption record list error: %v", err.Error())
	}

	var redemptionRecords []types.RedemptionRecord
	for _, item := range list {
		redemptionRecords = append(redemptionRecords, types.RedemptionRecord{
			Id:               item.Id,
			RedemptionCodeId: item.RedemptionCodeId,
			UserId:           item.UserId,
			SubscribeId:      item.SubscribeId,
			UnitTime:         item.UnitTime,
			Quantity:         item.Quantity,
			RedeemedAt:       item.RedeemedAt.Unix(),
			CreatedAt:        item.CreatedAt.Unix(),
		})
	}

	return &types.GetRedemptionRecordListResponse{
		Total: total,
		List:  redemptionRecords,
	}, nil
}
