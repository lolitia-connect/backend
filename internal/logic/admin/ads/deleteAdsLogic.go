package ads

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteAdsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete Ads
func NewDeleteAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAdsLogic {
	return &DeleteAdsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteAdsLogic) DeleteAds(req *types.DeleteAdsRequest) error {
	if err := l.svcCtx.Store.Ads().Delete(l.ctx, req.Id); err != nil {
		l.Logger.Errorw("delete ads error", zap.Any("error", err.Error()), zap.Any("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete ads error: %v", err.Error())
	}
	return nil
}
