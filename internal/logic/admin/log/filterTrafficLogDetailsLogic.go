package log

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/traffic"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type FilterTrafficLogDetailsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterTrafficLogDetailsLogic Filter traffic log details
func NewFilterTrafficLogDetailsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterTrafficLogDetailsLogic {
	return &FilterTrafficLogDetailsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterTrafficLogDetailsLogic) FilterTrafficLogDetails(req *types.FilterTrafficLogDetailsRequest) (resp *types.FilterTrafficLogDetailsResponse, err error) {
	var start, end time.Time
	if req.Date != "" {
		day, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
		if err != nil {
			l.Logger.Errorw("[FilterTrafficLogDetails] Date Parse Error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), " date parse error: %s", err.Error())
		}
		start = day
		end = day.Add(24 * time.Hour)
	} else {
		// query today
		now := time.Now()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end = start.Add(24 * time.Hour)
	}
	data, total, err := l.svcCtx.Store.TrafficLog().QueryTrafficLogDetails(l.ctx, &traffic.TrafficLogDetailsFilter{
		ServerId:    req.ServerId,
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		Start:       start,
		End:         end,
		Page:        req.Page,
		Size:        req.Size,
	})
	if err != nil {
		l.Logger.Errorw("[FilterTrafficLogDetails] Query Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " database query error: %s", err.Error())
	}

	var logs []types.TrafficLogDetails
	for _, v := range data {
		logs = append(logs, types.TrafficLogDetails{
			Id:          v.Id,
			UserId:      v.UserId,
			ServerId:    v.ServerId,
			SubscribeId: v.SubscribeId,
			Download:    v.Download,
			Upload:      v.Upload,
			Timestamp:   v.Timestamp.UnixMilli(),
		})
	}

	return &types.FilterTrafficLogDetailsResponse{
		List:  logs,
		Total: total,
	}, nil
}
