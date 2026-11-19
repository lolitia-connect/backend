package log

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type FilterServerTrafficLogLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterServerTrafficLogLogic Filter server traffic log
func NewFilterServerTrafficLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterServerTrafficLogLogic {
	return &FilterServerTrafficLogLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}
func (l *FilterServerTrafficLogLogic) FilterServerTrafficLog(req *types.FilterServerTrafficLogRequest) (*types.FilterServerTrafficLogResponse, error) {
	if req.Size <= 0 {
		req.Size = 10
	}
	if req.Size > 100 {
		req.Size = 100
	}
	if req.Page <= 0 {
		req.Page = 1
	}

	now := time.Now()
	today := now.Format(time.DateOnly)
	filter := &log.FilterParams{
		Type:      log.TypeServerTraffic.Uint8(),
		ObjectID:  req.ServerId,
		Data:      req.Date,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	}
	var list []types.ServerTrafficLog
	if trafficDateIncluded(filter, today) {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		serverTraffic, err := l.svcCtx.Store.TrafficLog().QueryServerTrafficRanking(l.ctx, start, start.AddDate(0, 0, 1))
		if err != nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "today traffic query error: %s", err.Error())
		}
		for _, item := range serverTraffic {
			if req.ServerId != 0 && item.ServerId != req.ServerId {
				continue
			}
			list = append(list, types.ServerTrafficLog{
				ServerId: item.ServerId, Upload: item.Upload, Download: item.Download,
				Total: item.Total, Date: today, Details: true,
			})
		}
	}

	todayTotal := len(list)
	startIdx := (req.Page - 1) * req.Size
	if startIdx < 0 {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	pageData := make([]types.ServerTrafficLog, 0, req.Size)
	if startIdx < todayTotal {
		pageData = append(pageData, list[startIdx:min(startIdx+req.Size, todayTotal)]...)
	}
	historyOffset := max(startIdx-todayTotal, 0)
	need := req.Size - len(pageData)
	filter.Page, filter.Size, filter.Offset = 1, max(need, 1), &historyOffset
	historyData, historyTotal, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, filter)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "history query error: %s", err.Error())
	}
	for _, item := range historyData {
		if need == 0 {
			break
		}
		var content log.ServerTraffic
		if err := content.Unmarshal([]byte(item.Content)); err != nil {
			l.Logger.Errorw("[FilterServerTrafficLog] Unmarshal Error", zap.Error(err))
			continue
		}
		hasDetails := true
		if l.svcCtx.Config.Log.AutoClear {
			day, err := time.ParseInLocation(time.DateOnly, item.Date, time.Local)
			hasDetails = err == nil && !day.Before(now.AddDate(0, 0, -int(l.svcCtx.Config.Log.ClearDays)))
		}
		pageData = append(pageData, types.ServerTrafficLog{
			ServerId: item.ObjectID, Upload: content.Upload, Download: content.Download,
			Total: content.Total, Date: item.Date, Details: hasDetails,
		})
	}
	return &types.FilterServerTrafficLogResponse{List: pageData, Total: int64(todayTotal) + historyTotal}, nil
}
