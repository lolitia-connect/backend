package ads

import (
	"context"
	"time"

	entads "github.com/perfect-panel/server/ent/ads"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Ads
func NewUpdateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAdsLogic {
	return &UpdateAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAdsLogic) UpdateAds(req *types.UpdateAdsRequest) error {
	_, err := l.svcCtx.Ent.Ads.Query().Where(entads.ID(req.Id)).Only(l.ctx)
	if err != nil {
		l.Errorw("find ads error", logger.Field("error", err.Error()), logger.Field("id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ads error: %v", err.Error())
	}
	if err := l.svcCtx.Ent.Ads.UpdateOneID(req.Id).
		SetTitle(req.Title).
		SetType(req.Type).
		SetContent(req.Content).
		SetDescription(req.Description).
		SetTargetURL(req.TargetURL).
		SetStartTime(time.UnixMilli(req.StartTime)).
		SetEndTime(time.UnixMilli(req.EndTime)).
		SetStatus(req.Status).
		Exec(l.ctx); err != nil {
		l.Errorw("update ads error", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ads error: %v", err.Error())
	}
	return nil
}
