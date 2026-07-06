package ads

import (
	"context"

	entads "github.com/perfect-panel/server/ent/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetAdsListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Ads List
func NewGetAdsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdsListLogic {
	return &GetAdsListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdsListLogic) GetAdsList(req *types.GetAdsListRequest) (resp *types.GetAdsListResponse, err error) {
	query := l.svcCtx.Ent.Ads.Query()
	if req.Status != nil {
		query = query.Where(entads.Status(*req.Status))
	}
	if req.Search != "" {
		query = query.Where(entads.Or(entads.TitleContains(req.Search), entads.ContentContains(req.Search)))
	}
	total, err := query.Count(l.ctx)
	if err != nil {
		l.Errorw("get ads list error", logger.Field("error", err.Error()), logger.Field("req", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ads list error: %v", err.Error())
	}
	data, err := query.Offset((req.Page - 1) * req.Size).Limit(req.Size).All(l.ctx)
	if err != nil {
		l.Errorw("get ads list error", logger.Field("error", err.Error()), logger.Field("req", req))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ads list error: %v", err.Error())
	}
	resp = &types.GetAdsListResponse{
		Total: int64(total),
		List:  make([]types.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return
}
