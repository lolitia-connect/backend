package user

import (
	"context"
	"sort"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetUserSubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe
func NewGetUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeLogic {
	return &GetUserSubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeLogic) GetUserSubscribe(req *types.GetUserSubscribeListRequest) (resp *types.GetUserSubscribeListResponse, err error) {
	data, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, req.UserId, 0, 1, 2, 3, 4, 5)
	if err != nil {
		l.Logger.Errorw("[GetUserSubscribeLogs] Get User Subscribe Error:", zap.Any("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Subscribe Error")
	}

	resp = &types.GetUserSubscribeListResponse{
		List:  make([]types.UserSubscribe, 0),
		Total: int64(len(data)),
	}

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 10
	}
	if req.Page-1 > len(data)/req.Size {
		return resp, nil
	}
	sort.Slice(data, func(i, j int) bool { return data[i].Id < data[j].Id })
	start := (req.Page - 1) * req.Size
	for _, item := range data[start:min(start+req.Size, len(data))] {
		var sub types.UserSubscribe
		tool.DeepCopy(&sub, item)
		sub.Short, _ = tool.FixedUniqueString(item.Token, 8, "")
		resp.List = append(resp.List, sub)
	}
	return
}
