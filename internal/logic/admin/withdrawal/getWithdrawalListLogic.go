package withdrawal

import (
	"context"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetWithdrawalListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	Logger *zap.SugaredLogger
}

func NewGetWithdrawalListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWithdrawalListLogic {
	return &GetWithdrawalListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: zap.S(),
	}
}

// GetWithdrawalList pages the withdrawal review queue, optionally narrowed to
// one status or one user.
func (l *GetWithdrawalListLogic) GetWithdrawalList(req *types.GetWithdrawalListRequest) (*types.GetWithdrawalListResponse, error) {
	page, size := normalizePage(req.Page, req.Size)

	list, total, err := l.svcCtx.Store.User().QueryWithdrawalPageList(l.ctx, &user.WithdrawalFilter{
		UserId: req.UserId,
		Status: req.Status,
	}, page, size)
	if err != nil {
		l.Logger.Errorf("Failed to query withdrawal list: %v", err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to query withdrawal list: %v", err)
	}

	resp := &types.GetWithdrawalListResponse{
		Total: total,
		List:  make([]types.WithdrawalLog, 0, len(list)),
	}
	for _, item := range list {
		resp.List = append(resp.List, *withdrawalToType(item))
	}
	return resp, nil
}
