package subscribe

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetSubscribeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe list
func NewGetSubscribeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeListLogic {
	return &GetSubscribeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeListLogic) GetSubscribeList(req *types.GetSubscribeListRequest) (resp *types.GetSubscribeListResponse, err error) {
	// Build filter params
	filterParams := &subscribe.FilterParams{
		Page:     int(req.Page),
		Size:     int(req.Size),
		Language: req.Language,
		Search:   req.Search,
	}

	// Add NodeGroupId filter if provided
	if req.NodeGroupId > 0 {
		filterParams.NodeGroupId = &req.NodeGroupId
	}

	total, list, err := l.svcCtx.SubscribeModel.FilterList(l.ctx, filterParams)
	if err != nil {
		l.Logger.Error("[GetSubscribeListLogic] get subscribe list failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe list failed: %v", err.Error())
	}
	var (
		subscribeIdList = make([]int64, 0, len(list))
		resultList      = make([]types.SubscribeItem, 0, len(list))
	)
	for _, item := range list {
		subscribeIdList = append(subscribeIdList, item.Id)
		var sub types.SubscribeItem
		tool.DeepCopy(&sub, item)
		if item.Discount != "" {
			err = json.Unmarshal([]byte(item.Discount), &sub.Discount)
			if err != nil {
				l.Logger.Error("[GetSubscribeListLogic] JSON unmarshal failed: ", logger.Field("error", err.Error()), logger.Field("discount", item.Discount))
			}
		}
		sub.Nodes = tool.StringToInt64Slice(item.Nodes)
		sub.NodeTags = strings.Split(item.NodeTags, ",")
		// Handle NodeGroupIds - convert from JSONInt64Slice to []int64
		if item.NodeGroupIds != nil {
			sub.NodeGroupIds = []int64(item.NodeGroupIds)
		} else {
			sub.NodeGroupIds = []int64{}
		}
		// NodeGroupId is already int64, should be copied by DeepCopy
		sub.NodeGroupId = item.NodeGroupId
		resultList = append(resultList, sub)
	}

	subscribeMaps, err := l.svcCtx.UserModel.QueryActiveSubscriptions(l.ctx, subscribeIdList...)
	if err != nil {
		l.Logger.Error("[GetSubscribeListLogic] get user subscribe failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get user subscribe failed: %v", err.Error())
	}

	for i, item := range resultList {
		if sub, ok := subscribeMaps[item.Id]; ok {
			resultList[i].Sold = sub
		}
	}

	resp = &types.GetSubscribeListResponse{
		Total: total,
		List:  resultList,
	}
	return
}
