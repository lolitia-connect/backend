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

type CreateAdsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create Ads
func NewCreateAdsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAdsLogic {
	return &CreateAdsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateAdsLogic) CreateAds(req *types.CreateAdsRequest) error {
	if err := l.svcCtx.Store.Ads().Insert(l.ctx, &model.Ads{Title: req.Title, Type: req.Type, Content: req.Content, Description: req.Description, TargetURL: req.TargetURL, StartTime: time.UnixMilli(req.StartTime), EndTime: time.UnixMilli(req.EndTime), Status: req.Status}); err != nil {
		l.Logger.Errorw("insert ads error: %v", zap.Any("error", err.Error()), zap.Any("req", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert ads error: %v", err.Error())
	}
	return nil
}
