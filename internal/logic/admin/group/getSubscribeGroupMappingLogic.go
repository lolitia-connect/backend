package group

import (
	"context"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetSubscribeGroupMappingLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe group mapping
func NewGetSubscribeGroupMappingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeGroupMappingLogic {
	return &GetSubscribeGroupMappingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeGroupMappingLogic) GetSubscribeGroupMapping(req *types.GetSubscribeGroupMappingRequest) (resp *types.GetSubscribeGroupMappingResponse, err error) {
	// 1. 查询所有订阅套餐
	var subscribes []subscribe.Subscribe
	if err := l.svcCtx.DB.Model(&subscribe.Subscribe{}).Find(&subscribes).Error; err != nil {
		l.Errorw("[GetSubscribeGroupMapping] failed to query subscribes", logger.Field("error", err.Error()))
		return nil, err
	}

	// 2. 查询所有节点组
	var nodeGroups []group.NodeGroup
	if err := l.svcCtx.DB.Model(&group.NodeGroup{}).Find(&nodeGroups).Error; err != nil {
		l.Errorw("[GetSubscribeGroupMapping] failed to query node groups", logger.Field("error", err.Error()))
		return nil, err
	}

	// 创建 node_group_id -> node_group_name 的映射
	nodeGroupMap := make(map[int64]string)
	for _, ng := range nodeGroups {
		nodeGroupMap[ng.Id] = ng.Name
	}

	// 3. 构建映射结果：套餐 -> 默认节点组（一对一）
	var mappingList []types.SubscribeGroupMappingItem

	for _, sub := range subscribes {
		// 获取套餐的默认节点组（node_group_ids 数组的第一个）
		nodeGroupName := ""
		if len(sub.NodeGroupIds) > 0 {
			defaultNodeGroupId := sub.NodeGroupIds[0]
			nodeGroupName = nodeGroupMap[defaultNodeGroupId]
		}

		mappingList = append(mappingList, types.SubscribeGroupMappingItem{
			SubscribeName: sub.Name,
			NodeGroupName: nodeGroupName,
		})
	}

	resp = &types.GetSubscribeGroupMappingResponse{
		List: mappingList,
	}

	return resp, nil
}
