package group

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type PreviewUserNodesLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPreviewUserNodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreviewUserNodesLogic {
	return &PreviewUserNodesLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PreviewUserNodesLogic) PreviewUserNodes(req *types.PreviewUserNodesRequest) (resp *types.PreviewUserNodesResponse, err error) {
	logger.Infof("[PreviewUserNodes] userId: %v", req.UserId)

	// 1. 查询用户的所有有效订阅（只查询可用状态：0-Pending, 1-Active）
	type UserSubscribe struct {
		Id          int64
		UserId      int64
		SubscribeId int64
		NodeGroupId int64 // 用户订阅的 node_group_id（单个ID）
	}
	var userSubscribes []UserSubscribe
	err = l.svcCtx.DB.Table("user_subscribe").
		Select("id, user_id, subscribe_id, node_group_id").
		Where("user_id = ? AND status IN ?", req.UserId, []int8{0, 1}).
		Find(&userSubscribes).Error
	if err != nil {
		logger.Errorf("[PreviewUserNodes] failed to get user subscribes: %v", err)
		return nil, err
	}

	if len(userSubscribes) == 0 {
		logger.Infof("[PreviewUserNodes] no user subscribes found")
		resp = &types.PreviewUserNodesResponse{
			UserId:     req.UserId,
			NodeGroups: []types.NodeGroupItem{},
		}
		return resp, nil
	}

	logger.Infof("[PreviewUserNodes] found %v user subscribes", len(userSubscribes))

	// 2. 按优先级获取 node_group_id：user_subscribe.node_group_id > subscribe.node_group_id > subscribe.node_group_ids[0]
	// 收集所有订阅ID以便批量查询
	subscribeIds := make([]int64, len(userSubscribes))
	for i, us := range userSubscribes {
		subscribeIds[i] = us.SubscribeId
	}

	// 批量查询订阅信息
	type SubscribeInfo struct {
		Id           int64
		NodeGroupId  int64
		NodeGroupIds string // JSON string
	}
	var subscribeInfos []SubscribeInfo
	err = l.svcCtx.DB.Table("subscribe").
		Select("id, node_group_id, node_group_ids").
		Where("id IN ?", subscribeIds).
		Find(&subscribeInfos).Error
	if err != nil {
		logger.Errorf("[PreviewUserNodes] failed to get subscribe infos: %v", err)
		return nil, err
	}

	// 创建 subscribe_id -> SubscribeInfo 的映射
	subInfoMap := make(map[int64]SubscribeInfo)
	for _, si := range subscribeInfos {
		subInfoMap[si.Id] = si
	}

	// 按优先级获取每个用户订阅的 node_group_id
	var allNodeGroupIds []int64
	for _, us := range userSubscribes {
		nodeGroupId := int64(0)

		// 优先级1: user_subscribe.node_group_id
		if us.NodeGroupId != 0 {
			nodeGroupId = us.NodeGroupId
			logger.Debugf("[PreviewUserNodes] user_subscribe_id=%d using node_group_id=%d", us.Id, nodeGroupId)
		} else {
			// 优先级2: subscribe.node_group_id
			subInfo, ok := subInfoMap[us.SubscribeId]
			if ok {
				if subInfo.NodeGroupId != 0 {
					nodeGroupId = subInfo.NodeGroupId
					logger.Debugf("[PreviewUserNodes] user_subscribe_id=%d using subscribe.node_group_id=%d", us.Id, nodeGroupId)
				} else if subInfo.NodeGroupIds != "" && subInfo.NodeGroupIds != "null" && subInfo.NodeGroupIds != "[]" {
					// 优先级3: subscribe.node_group_ids[0]
					var nodeGroupIds []int64
					if err := json.Unmarshal([]byte(subInfo.NodeGroupIds), &nodeGroupIds); err == nil && len(nodeGroupIds) > 0 {
						nodeGroupId = nodeGroupIds[0]
						logger.Debugf("[PreviewUserNodes] user_subscribe_id=%d using subscribe.node_group_ids[0]=%d", us.Id, nodeGroupId)
					}
				}
			}
		}

		if nodeGroupId != 0 {
			allNodeGroupIds = append(allNodeGroupIds, nodeGroupId)
		}
	}

	// 去重
	allNodeGroupIds = removeDuplicateInt64(allNodeGroupIds)

	logger.Infof("[PreviewUserNodes] collected node_group_ids with priority: %v", allNodeGroupIds)

	// 4. 判断分组功能是否启用
	var groupEnabled string
	l.svcCtx.DB.Table("system").
		Where("`category` = ? AND `key` = ?", "group", "enabled").
		Select("value").
		Scan(&groupEnabled)

	logger.Infof("[PreviewUserNodes] groupEnabled: %v", groupEnabled)

	isGroupEnabled := groupEnabled == "true" || groupEnabled == "1"

	var filteredNodes []node.Node

	if isGroupEnabled {
		// === 启用分组功能：通过用户订阅的 node_group_id 查询节点 ===
		logger.Infof("[PreviewUserNodes] using group-based node filtering")

		if len(allNodeGroupIds) == 0 {
			logger.Infof("[PreviewUserNodes] no node groups found in user subscribes")
			resp = &types.PreviewUserNodesResponse{
				UserId:     req.UserId,
				NodeGroups: []types.NodeGroupItem{},
			}
			return resp, nil
		}

		// 5. 查询所有启用的节点
		var dbNodes []node.Node
		err = l.svcCtx.DB.Table("nodes").
			Where("enabled = ?", true).
			Find(&dbNodes).Error
		if err != nil {
			logger.Errorf("[PreviewUserNodes] failed to get nodes: %v", err)
			return nil, err
		}

		// 6. 过滤出包含至少一个匹配节点组的节点
		// node_group_ids 为空 = 公共节点，所有人可见
		// node_group_ids 与订阅的 node_group_id 匹配 = 该节点可见
		for _, n := range dbNodes {
			// 公共节点（node_group_ids 为空），所有人可见
			if len(n.NodeGroupIds) == 0 {
				filteredNodes = append(filteredNodes, n)
				continue
			}

			// 检查节点的 node_group_ids 是否与订阅的 node_group_id 有交集
			for _, nodeGroupId := range n.NodeGroupIds {
				if tool.Contains(allNodeGroupIds, nodeGroupId) {
					filteredNodes = append(filteredNodes, n)
					break
				}
			}
		}

		logger.Infof("[PreviewUserNodes] found %v nodes using group filter", len(filteredNodes))

	} else {
		// === 未启用分组功能：通过订阅的 node_tags 查询节点 ===
		logger.Infof("[PreviewUserNodes] using tag-based node filtering")

		// 5. 获取所有订阅的 subscribeId 列表
		subscribeIds := make([]int64, len(userSubscribes))
		for i, us := range userSubscribes {
			subscribeIds[i] = us.SubscribeId
		}

		// 6. 查询这些订阅的 node_tags
		type SubscribeNodeTags struct {
			Id       int64
			NodeTags string
		}
		var subscribeNodeTagsList []SubscribeNodeTags
		err = l.svcCtx.DB.Table("subscribe").
			Where("id IN ?", subscribeIds).
			Select("id, node_tags").
			Find(&subscribeNodeTagsList).Error
		if err != nil {
			logger.Errorf("[PreviewUserNodes] failed to get subscribe node tags: %v", err)
			return nil, err
		}

		// 7. 合并所有标签
		var allTags []string
		for _, snt := range subscribeNodeTagsList {
			if snt.NodeTags != "" {
				tags := strings.Split(snt.NodeTags, ",")
				allTags = append(allTags, tags...)
			}
		}
		// 去重
		allTags = tool.RemoveDuplicateElements(allTags...)
		// 去除空字符串
		allTags = tool.RemoveStringElement(allTags, "")

		logger.Infof("[PreviewUserNodes] merged tags from subscribes: %v", allTags)

		if len(allTags) == 0 {
			logger.Infof("[PreviewUserNodes] no tags found in subscribes")
			resp = &types.PreviewUserNodesResponse{
				UserId:     req.UserId,
				NodeGroups: []types.NodeGroupItem{},
			}
			return resp, nil
		}

		// 8. 查询所有启用的节点
		var dbNodes []node.Node
		err = l.svcCtx.DB.Table("nodes").
			Where("enabled = ?", true).
			Find(&dbNodes).Error
		if err != nil {
			logger.Errorf("[PreviewUserNodes] failed to get nodes: %v", err)
			return nil, err
		}

		// 9. 过滤出包含至少一个匹配标签的节点
		for _, n := range dbNodes {
			if n.Tags == "" {
				continue
			}
			nodeTags := strings.Split(n.Tags, ",")
			// 检查是否有交集
			for _, tag := range nodeTags {
				if tag != "" && tool.Contains(allTags, tag) {
					filteredNodes = append(filteredNodes, n)
					break
				}
			}
		}

		logger.Infof("[PreviewUserNodes] found %v nodes using tag filter", len(filteredNodes))
	}

	// 10. 转换为 types.Node 并按节点组分组
	type NodeWithGroup struct {
		Node         node.Node
		NodeGroupIds []int64
	}

	nodesWithGroup := make([]NodeWithGroup, 0, len(filteredNodes))
	for _, n := range filteredNodes {
		nodesWithGroup = append(nodesWithGroup, NodeWithGroup{
			Node:         n,
			NodeGroupIds: []int64(n.NodeGroupIds),
		})
	}

	// 11. 按节点组分组节点
	type NodeGroupMap struct {
		Id    int64
		Nodes []types.Node
	}

	// 创建节点组映射：group_id -> nodes
	groupMap := make(map[int64]*NodeGroupMap)

	// 获取所有涉及的节点组ID
	allGroupIds := make([]int64, 0)
	for _, ng := range nodesWithGroup {
		if len(ng.NodeGroupIds) > 0 {
			// 如果节点属于节点组，按第一个节点组分组（或者可以按所有节点组）
			// 这里使用节点的第一个节点组
			firstGroupId := ng.NodeGroupIds[0]
			if _, exists := groupMap[firstGroupId]; !exists {
				groupMap[firstGroupId] = &NodeGroupMap{
					Id:    firstGroupId,
					Nodes: []types.Node{},
				}
				allGroupIds = append(allGroupIds, firstGroupId)
			}

			// 转换节点
			tags := []string{}
			if ng.Node.Tags != "" {
				tags = strings.Split(ng.Node.Tags, ",")
			}
			node := types.Node{
				Id:           ng.Node.Id,
				Name:         ng.Node.Name,
				Tags:         tags,
				Port:         ng.Node.Port,
				Address:      ng.Node.Address,
				ServerId:     ng.Node.ServerId,
				Protocol:     ng.Node.Protocol,
				Enabled:      ng.Node.Enabled,
				Sort:         ng.Node.Sort,
				NodeGroupIds: []int64(ng.Node.NodeGroupIds),
				CreatedAt:    ng.Node.CreatedAt.Unix(),
				UpdatedAt:    ng.Node.UpdatedAt.Unix(),
			}

			groupMap[firstGroupId].Nodes = append(groupMap[firstGroupId].Nodes, node)
		} else {
			// 没有节点组的节点，使用 group_id = 0 作为"无节点组"分组
			if _, exists := groupMap[0]; !exists {
				groupMap[0] = &NodeGroupMap{
					Id:    0,
					Nodes: []types.Node{},
				}
			}

			tags := []string{}
			if ng.Node.Tags != "" {
				tags = strings.Split(ng.Node.Tags, ",")
			}
			node := types.Node{
				Id:           ng.Node.Id,
				Name:         ng.Node.Name,
				Tags:         tags,
				Port:         ng.Node.Port,
				Address:      ng.Node.Address,
				ServerId:     ng.Node.ServerId,
				Protocol:     ng.Node.Protocol,
				Enabled:      ng.Node.Enabled,
				Sort:         ng.Node.Sort,
				NodeGroupIds: []int64(ng.Node.NodeGroupIds),
				CreatedAt:    ng.Node.CreatedAt.Unix(),
				UpdatedAt:    ng.Node.UpdatedAt.Unix(),
			}

			groupMap[0].Nodes = append(groupMap[0].Nodes, node)
		}
	}

	// 12. 查询节点组信息并构建响应
	nodeGroupInfoMap := make(map[int64]string)
	validGroupIds := make([]int64, 0) // 存储在数据库中实际存在的节点组ID

	if len(allGroupIds) > 0 {
		type NodeGroupInfo struct {
			Id   int64
			Name string
		}
		var nodeGroupInfos []NodeGroupInfo
		err = l.svcCtx.DB.Table("node_group").
			Select("id, name").
			Where("id IN ?", allGroupIds).
			Find(&nodeGroupInfos).Error
		if err != nil {
			logger.Errorf("[PreviewUserNodes] failed to get node group infos: %v", err)
			return nil, err
		}

		logger.Infof("[PreviewUserNodes] found %v node group infos from %v requested", len(nodeGroupInfos), len(allGroupIds))

		// 创建节点组信息映射和有效节点组ID列表
		for _, ngInfo := range nodeGroupInfos {
			nodeGroupInfoMap[ngInfo.Id] = ngInfo.Name
			validGroupIds = append(validGroupIds, ngInfo.Id)
			logger.Debugf("[PreviewUserNodes] node_group[%d] = %s", ngInfo.Id, ngInfo.Name)
		}

		// 记录无效的节点组ID（节点有这个ID但数据库中不存在）
		for _, requestedId := range allGroupIds {
			found := false
			for _, validId := range validGroupIds {
				if requestedId == validId {
					found = true
					break
				}
			}
			if !found {
				logger.Infof("[PreviewUserNodes] node_group_id %d not found in database, treating as public nodes", requestedId)
			}
		}
	}

	// 13. 构建响应：根据有效节点组ID重新分组节点
	nodeGroupItems := make([]types.NodeGroupItem, 0)
	publicNodes := make([]types.Node, 0) // 公共节点（包括无效节点组和无节点组的节点）

	// 遍历所有分组，重新分类节点
	for groupId, gm := range groupMap {
		if groupId == 0 {
			// 本来就是无节点组的节点
			publicNodes = append(publicNodes, gm.Nodes...)
			continue
		}

		// 检查这个节点组ID是否有效（在数据库中存在）
		isValid := false
		for _, validId := range validGroupIds {
			if groupId == validId {
				isValid = true
				break
			}
		}

		if isValid {
			// 节点组有效，添加到对应的分组
			groupName := nodeGroupInfoMap[groupId]
			if groupName == "" {
				groupName = fmt.Sprintf("Group %d", groupId)
			}
			nodeGroupItems = append(nodeGroupItems, types.NodeGroupItem{
				Id:    groupId,
				Name:  groupName,
				Nodes: gm.Nodes,
			})
			logger.Infof("[PreviewUserNodes] adding node group: id=%d, name=%s, nodes=%d", groupId, groupName, len(gm.Nodes))
		} else {
			// 节点组无效，节点归入公共节点组
			logger.Infof("[PreviewUserNodes] node_group_id %d invalid, moving %d nodes to public group", groupId, len(gm.Nodes))
			publicNodes = append(publicNodes, gm.Nodes...)
		}
	}

	// 最后添加公共节点组（如果有）
	if len(publicNodes) > 0 {
		nodeGroupItems = append(nodeGroupItems, types.NodeGroupItem{
			Id:    0,
			Name:  "",
			Nodes: publicNodes,
		})
		logger.Infof("[PreviewUserNodes] adding public group: nodes=%d", len(publicNodes))
	}

	// 14. 返回结果
	resp = &types.PreviewUserNodesResponse{
		UserId:     req.UserId,
		NodeGroups: nodeGroupItems,
	}

	logger.Infof("[PreviewUserNodes] returning %v node groups for user %v", len(resp.NodeGroups), req.UserId)
	return resp, nil
}

// removeDuplicateInt64 去重 []int64
func removeDuplicateInt64(slice []int64) []int64 {
	keys := make(map[int64]bool)
	var list []int64
	for _, entry := range slice {
		if !keys[entry] {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
