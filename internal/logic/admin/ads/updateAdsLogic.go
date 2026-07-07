package ads

import (
	"context"
	"time"

	model "github.com/perfect-panel/server/internal/model/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateAdsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Ads
func NewUpdateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAdsLogic {
	return &UpdateAdsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAdsLogic) UpdateAds(req *types.UpdateAdsRequest) error {
	_, err := l.svcCtx.Store.Ads().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("find ads error", zap.Any("error", err.Error()), zap.Any("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ads error: %v", err.Error())
	}
	if err := l.svcCtx.Store.Ads().Update(l.ctx, &model.Ads{Id: req.Id, Title: req.Title, Type: req.Type, Content: req.Content, Description: req.Description, TargetURL: req.TargetURL, StartTime: time.UnixMilli(req.StartTime), EndTime: time.UnixMilli(req.EndTime), Status: req.Status}); err != nil {
		l.Logger.Errorw("update ads error", zap.Any("error", err.Error()), zap.Any("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ads error: %v", err.Error())
	}
	return nil
}
