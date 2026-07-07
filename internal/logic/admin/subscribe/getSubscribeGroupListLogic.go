package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetSubscribeGroupListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe group list
func NewGetSubscribeGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeGroupListLogic {
	return &GetSubscribeGroupListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeGroupListLogic) GetSubscribeGroupList() (resp *types.GetSubscribeGroupListResponse, err error) {
	total, list, err := l.svcCtx.Store.Subscribe().QueryGroupList(l.ctx)
	if err != nil {
		l.Logger.Error("[GetSubscribeGroupListLogic] get subscribe group list failed: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe group list failed: %v", err.Error())
	}
	groupList := make([]types.SubscribeGroup, 0)
	tool.DeepCopy(&groupList, list)
	return &types.GetSubscribeGroupListResponse{
		Total: total,
		List:  groupList,
	}, nil
}
