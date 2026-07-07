package user

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetUserTrafficStatsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get User Traffic Statistics
func NewGetUserTrafficStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserTrafficStatsLogic {
	return &GetUserTrafficStatsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserTrafficStatsLogic) GetUserTrafficStats(req *types.GetUserTrafficStatsRequest) (resp *types.GetUserTrafficStatsResponse, err error) {
	// 获取当前用户
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// 将字符串 ID 转换为 int64
	userSubscribeId, err := strconv.ParseInt(req.UserSubscribeId, 10, 64)
	if err != nil {
		l.Logger.Errorw("[GetUserTrafficStats] Invalid User Subscribe ID:",
			zap.Any("user_subscribe_id", req.UserSubscribeId),
			zap.Any("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid subscription ID")
	}

	// 验证订阅归属权 - 直接查询 user_subscribe 表
	userSubscribe, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, userSubscribeId)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Errorw("[GetUserTrafficStats] User Subscribe Not Found:",
				zap.Any("user_subscribe_id", userSubscribeId),
				zap.Any("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Subscription not found")
		}
		l.Logger.Errorw("[GetUserTrafficStats] Query User Subscribe Error:", zap.Any("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Subscribe Error")
	}

	if userSubscribe.UserId != u.Id {
		l.Logger.Errorw("[GetUserTrafficStats] User Subscribe Access Denied:",
			zap.Any("user_subscribe_id", userSubscribeId),
			zap.Any("subscribe_user_id", userSubscribe.UserId),
			zap.Any("current_user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// 计算时间范围
	now := time.Now()
	startDate := now.AddDate(0, 0, -req.Days+1)
	startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, time.Local)

	// 初始化响应
	resp = &types.GetUserTrafficStatsResponse{
		List:          make([]types.DailyTrafficStats, 0, req.Days),
		TotalUpload:   0,
		TotalDownload: 0,
		TotalTraffic:  0,
	}

	// 按天查询流量数据
	for i := 0; i < req.Days; i++ {
		currentDate := startDate.AddDate(0, 0, i)
		dayStart := time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 0, 0, 0, 0, time.Local)
		dayEnd := dayStart.Add(24 * time.Hour).Add(-time.Nanosecond)

		dailyTraffic, err := l.svcCtx.Store.TrafficLog().SumByUserSubscribeAndTimeRange(l.ctx, u.Id, userSubscribeId, dayStart, dayEnd)
		if err != nil {
			l.Logger.Errorw("[GetUserTrafficStats] Query Daily Traffic Error:",
				zap.Any("date", currentDate.Format("2006-01-02")),
				zap.Any("err", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query Traffic Error")
		}
		upload, download := int64(0), int64(0)
		if dailyTraffic != nil {
			upload = dailyTraffic.Upload
			download = dailyTraffic.Download
		}

		// 添加到结果列表
		total := upload + download
		resp.List = append(resp.List, types.DailyTrafficStats{
			Date:     currentDate.Format("2006-01-02"),
			Upload:   upload,
			Download: download,
			Total:    total,
		})

		// 累加总计
		resp.TotalUpload += upload
		resp.TotalDownload += download
	}

	resp.TotalTraffic = resp.TotalUpload + resp.TotalDownload

	return resp, nil
}
