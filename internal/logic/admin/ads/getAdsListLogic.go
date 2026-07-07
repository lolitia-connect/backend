package ads

import (
	"context"

	model "github.com/perfect-panel/server/internal/model/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetAdsListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Ads List
func NewGetAdsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdsListLogic {
	return &GetAdsListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdsListLogic) GetAdsList(req *types.GetAdsListRequest) (resp *types.GetAdsListResponse, err error) {
	total, data, err := l.svcCtx.Store.Ads().GetAdsListByPage(l.ctx, req.Page, req.Size, model.Filter{Status: req.Status, Search: req.Search})
	if err != nil {
		l.Logger.Errorw("get ads list error", zap.Any("error", err.Error()), zap.Any("req", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ads list error: %v", err.Error())
	}
	resp = &types.GetAdsListResponse{
		Total: total,
		List:  make([]types.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return
}
