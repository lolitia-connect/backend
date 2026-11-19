package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetUserSubscribeLogsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe logs
func NewGetUserSubscribeLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeLogsLogic {
	return &GetUserSubscribeLogsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeLogsLogic) GetUserSubscribeLogs(req *types.GetUserSubscribeLogsRequest) (resp *types.GetUserSubscribeLogsResponse, err error) {
	filter := &log.FilterParams{
		Page: req.Page, Size: req.Size,
		Type: log.TypeSubscribe.Uint8(), ObjectID: req.UserId,
	}
	if req.SubscribeId != 0 {
		filter.ContentInt64 = map[string]int64{"user_subscribe_id": req.SubscribeId}
	}
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, filter)

	if err != nil {
		l.Logger.Errorw("[GetUserSubscribeLogs] Get User Subscribe Logs Error:", zap.Any("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Subscribe Logs Error")
	}
	list := make([]types.UserSubscribeLog, 0, len(data))
	for _, item := range data {
		var content log.Subscribe
		if err := content.Unmarshal([]byte(item.Content)); err != nil {
			l.Logger.Errorf("[GetUserSubscribeLogs] unmarshal subscribe log failed: %v", err)
			continue
		}
		list = append(list, types.UserSubscribeLog{
			Id: item.Id, UserId: item.ObjectID, UserSubscribeId: content.UserSubscribeId,
			Token: content.Token, IP: content.ClientIP, UserAgent: content.UserAgent,
			Timestamp: item.CreatedAt.UnixMilli(),
		})
	}

	return &types.GetUserSubscribeLogsResponse{
		List:  list,
		Total: total,
	}, err
}
