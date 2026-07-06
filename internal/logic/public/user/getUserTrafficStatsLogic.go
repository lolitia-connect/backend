package user

import (
	"context"
	"fmt"
	"strconv"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/trafficlog"
	"github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserTrafficStatsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get User Traffic Statistics
func NewGetUserTrafficStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserTrafficStatsLogic {
	return &GetUserTrafficStatsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserTrafficStatsLogic) GetUserTrafficStats(req *types.GetUserTrafficStatsRequest) (resp *types.GetUserTrafficStatsResponse, err error) {
	// 获取当前用户
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	// 将字符串 ID 转换为 int64
	userSubscribeId, err := strconv.ParseInt(req.UserSubscribeId, 10, 64)
	if err != nil {
		l.Errorw("[GetUserTrafficStats] Invalid User Subscribe ID:",
			logger.Field("user_subscribe_id", req.UserSubscribeId),
			logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid subscription ID")
	}

	// 验证订阅归属权 - 直接查询 user_subscribe 表
	userSubscribe, err := l.svcCtx.Ent.UserSubscribe.Query().Where(usersubscribe.ID(userSubscribeId)).Only(l.ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Errorw("[GetUserTrafficStats] User Subscribe Not Found:",
				logger.Field("user_subscribe_id", userSubscribeId),
				logger.Field("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Subscription not found")
		}
		l.Errorw("[GetUserTrafficStats] Query User Subscribe Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Subscribe Error")
	}

	if userSubscribe.UserID != u.Id {
		l.Errorw("[GetUserTrafficStats] User Subscribe Access Denied:",
			logger.Field("user_subscribe_id", userSubscribeId),
			logger.Field("subscribe_user_id", userSubscribe.UserID),
			logger.Field("current_user_id", u.Id))
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

		var dailyTraffic []struct {
			Upload   int64 `json:"upload"`
			Download int64 `json:"download"`
		}
		err := l.svcCtx.Ent.TrafficLog.Query().Where(
			trafficlog.UserID(u.Id),
			trafficlog.SubscribeID(userSubscribeId),
			trafficlog.TimestampGTE(dayStart),
			trafficlog.TimestampLTE(dayEnd),
		).Aggregate(
			ent.As(sumTrafficField(trafficlog.FieldUpload), "upload"),
			ent.As(sumTrafficField(trafficlog.FieldDownload), "download"),
		).Scan(l.ctx, &dailyTraffic)
		if err != nil {
			l.Errorw("[GetUserTrafficStats] Query Daily Traffic Error:",
				logger.Field("date", currentDate.Format("2006-01-02")),
				logger.Field("err", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query Traffic Error")
		}
		upload, download := int64(0), int64(0)
		if len(dailyTraffic) > 0 {
			upload = dailyTraffic[0].Upload
			download = dailyTraffic[0].Download
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

func sumTrafficField(field string) ent.AggregateFunc {
	return func(s *entsql.Selector) string {
		return fmt.Sprintf("COALESCE(SUM(%s), 0)", s.C(field))
	}
}
