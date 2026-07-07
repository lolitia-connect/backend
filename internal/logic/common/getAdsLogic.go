package common

import (
	"context"

	model "github.com/perfect-panel/server/internal/model/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type GetAdsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Ads
func NewGetAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAdsLogic {
	return &GetAdsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAdsLogic) GetAds(req *types.GetAdsRequest) (resp *types.GetAdsResponse, err error) {
	// todo: add ads position and device
	status := 1
	_, data, err := l.svcCtx.Store.Ads().GetAdsListByPage(l.ctx, 1, 200, model.Filter{Status: &status})
	if err != nil {
		return nil, err
	}
	resp = &types.GetAdsResponse{
		List: make([]types.Ads, len(data)),
	}
	tool.DeepCopy(&resp.List, data)
	return
}
