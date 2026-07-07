package subscribe

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetSubscribeDetailsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe details
func NewGetSubscribeDetailsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeDetailsLogic {
	return &GetSubscribeDetailsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeDetailsLogic) GetSubscribeDetails(req *types.GetSubscribeDetailsRequest) (resp *types.Subscribe, err error) {
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[GetSubscribeDetailsLogic] get subscribe details failed: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe details failed: %v", err.Error())
	}
	resp = &types.Subscribe{}
	tool.DeepCopy(resp, sub)
	if sub.Discount != "" {
		err = json.Unmarshal([]byte(sub.Discount), &resp.Discount)
		if err != nil {
			l.Logger.Error("[GetSubscribeDetailsLogic] JSON unmarshal failed: ", zap.Any("error", err.Error()), zap.Any("discount", sub.Discount))
		}
	}
	resp.Nodes = types.StringInt64Slice(tool.StringToInt64Slice(sub.Nodes))
	resp.NodeTags = strings.Split(sub.NodeTags, ",")
	if sub.NodeGroupIds != nil {
		resp.NodeGroupIds = tool.Int64SliceToStringSlice([]int64(sub.NodeGroupIds))
	} else {
		resp.NodeGroupIds = []string{}
	}
	resp.NodeGroupId = sub.NodeGroupId
	return resp, nil
}
