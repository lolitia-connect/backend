package ads

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateAdsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create Ads
func NewCreateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAdsLogic {
	return &CreateAdsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateAdsLogic) CreateAds(req *types.CreateAdsRequest) error {
	if err := l.svcCtx.Ent.Ads.Create().
		SetTitle(req.Title).
		SetType(req.Type).
		SetContent(req.Content).
		SetDescription(req.Description).
		SetTargetURL(req.TargetURL).
		SetStartTime(time.UnixMilli(req.StartTime)).
		SetEndTime(time.UnixMilli(req.EndTime)).
		SetStatus(req.Status).
		Exec(l.ctx); err != nil {
		l.Errorw("insert ads error: %v", logger.Field("error", err.Error()), logger.Field("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert ads error: %v", err.Error())
	}
	return nil
}
