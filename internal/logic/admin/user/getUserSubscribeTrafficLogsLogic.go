package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/traffic"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetUserSubscribeTrafficLogsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe traffic logs
func NewGetUserSubscribeTrafficLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeTrafficLogsLogic {
	return &GetUserSubscribeTrafficLogsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeTrafficLogsLogic) GetUserSubscribeTrafficLogs(req *types.GetUserSubscribeTrafficLogsRequest) (resp *types.GetUserSubscribeTrafficLogsResponse, err error) {
	if req.UserId <= 0 || req.SubscribeId <= 0 || (req.StartTime != 0 && req.EndTime != 0 && req.EndTime < req.StartTime) {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	filter := &traffic.TrafficLogDetailsFilter{UserId: req.UserId, SubscribeId: req.SubscribeId, Page: req.Page, Size: req.Size}
	if req.StartTime != 0 {
		filter.Start = time.UnixMilli(req.StartTime)
	}
	if req.EndTime != 0 {
		filter.End = time.UnixMilli(req.EndTime)
	}
	list, total, err := l.svcCtx.Store.TrafficLog().QueryTrafficLogDetails(l.ctx, filter)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetUserSubscribeTrafficLogs failed: %v", err.Error())
	}
	userRespList := make([]types.TrafficLog, 0)
	tool.DeepCopy(&userRespList, list)
	return &types.GetUserSubscribeTrafficLogsResponse{
		Total: total,
		List:  userRespList,
	}, nil
}
