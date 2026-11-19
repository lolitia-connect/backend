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

type FilterUserSubscribeTrafficLogLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterUserSubscribeTrafficLogLogic Filter user subscribe traffic log
func NewFilterUserSubscribeTrafficLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterUserSubscribeTrafficLogLogic {
	return &FilterUserSubscribeTrafficLogLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterUserSubscribeTrafficLogLogic) FilterUserSubscribeTrafficLog(req *types.FilterSubscribeTrafficRequest) (resp *types.FilterSubscribeTrafficResponse, err error) {
	if req.Size <= 0 {
		req.Size = 10
	}
	if req.Size > 100 {
		req.Size = 100
	}
	if req.Page <= 0 {
		req.Page = 1
	}

	today := time.Now().Format("2006-01-02")
	var list []types.UserSubscribeTrafficLog
	var total int64
	contentFilters := make(map[string]int64)
	if req.UserId > 0 {
		contentFilters["user_id"] = req.UserId
	}
	if req.UserSubscribeId > 0 {
		contentFilters["subscribe_id"] = req.UserSubscribeId
	}

	dateFilter := &log.FilterParams{Data: req.Date, StartDate: req.StartDate, EndDate: req.EndDate}
	if trafficDateIncluded(dateFilter, today) {
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		end := start.AddDate(0, 0, 1)

		userTraffic, err := l.svcCtx.Store.TrafficLog().QueryUserTrafficRanking(l.ctx, start, end)
		if err != nil {
			l.Logger.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", zap.Any("error", err.Error()))
			return nil, err
		}

		for _, v := range userTraffic {
			if (req.UserId > 0 && v.UserId != req.UserId) || (req.UserSubscribeId > 0 && v.SubscribeId != req.UserSubscribeId) {
				continue
			}
			list = append(list, types.UserSubscribeTrafficLog{
				UserId:      v.UserId,
				SubscribeId: v.SubscribeId,
				Upload:      v.Upload,
				Download:    v.Download,
				Total:       v.Total,
				Date:        today,
				Details:     true,
			})
		}
		todayTotal := len(list)

		startIdx := (req.Page - 1) * req.Size
		pageData := make([]types.UserSubscribeTrafficLog, 0, req.Size)
		if startIdx < todayTotal {
			pageData = append(pageData, list[startIdx:min(startIdx+req.Size, todayTotal)]...)
		}
		historyOffset := max(startIdx-todayTotal, 0)
		need := req.Size - len(pageData)
		historyData, historyTotal, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
			Page:         1,
			Size:         max(need, 1),
			Offset:       &historyOffset,
			Type:         log.TypeSubscribeTraffic.Uint8(),
			Data:         req.Date,
			StartDate:    req.StartDate,
			EndDate:      req.EndDate,
			ContentInt64: contentFilters,
		})

		if err != nil {
			l.Logger.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterUserSubscribeTrafficLog] Query Database Error")
		}

		for _, datum := range historyData {
			if need == 0 {
				break
			}
			var item log.UserTraffic
			err = item.Unmarshal([]byte(datum.Content))
			if err != nil {
				l.Logger.Errorw("[FilterUserSubscribeTrafficLog] Unmarshal Content Error", zap.Any("error", err.Error()))
				continue
			}
			pageData = append(pageData, types.UserSubscribeTrafficLog{
				UserId:      item.UserId,
				SubscribeId: item.SubscribeId,
				Upload:      item.Upload,
				Download:    item.Download,
				Total:       item.Total,
				Date:        datum.Date,
				Details:     false,
			})
		}
		return &types.FilterSubscribeTrafficResponse{
			List:  pageData,
			Total: int64(todayTotal) + historyTotal,
		}, nil
	}
	var data []*log.SystemLog
	data, total, err = l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:         req.Page,
		Size:         req.Size,
		Type:         log.TypeSubscribeTraffic.Uint8(),
		Data:         req.Date,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		ContentInt64: contentFilters,
	})
	if err != nil {
		l.Logger.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterUserSubscribeTrafficLog] Query Database Error")
	}
	for _, datum := range data {
		var item log.UserTraffic
		err = item.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Logger.Errorw("[FilterUserSubscribeTrafficLog] Unmarshal Content Error", zap.Any("error", err.Error()))
			continue
		}
		list = append(list, types.UserSubscribeTrafficLog{
			UserId:      item.UserId,
			SubscribeId: item.SubscribeId,
			Upload:      item.Upload,
			Download:    item.Download,
			Total:       item.Total,
			Date:        datum.Date,
			Details:     false,
		})
	}
	return &types.FilterSubscribeTrafficResponse{
		List:  list,
		Total: total,
	}, nil
}
