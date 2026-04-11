package group

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
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
	err = l.svcCtx.DB.Model(&user.Subscribe{}).
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
		Nodes        string // JSON string - 直接分配的节点ID
		NodeTags     string // 节点标签
	}
	var subscribeInfos []SubscribeInfo
	err = l.svcCtx.DB.Model(&subscribe.Subscribe{}).
		Select("id, node_group_id, node_group_ids, nodes, node_tags").
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

	// 3. 收集所有订阅中直接分配的节点ID
	var allDirectNodeIds []int64
	for _, subInfo := range subscribeInfos {
		if subInfo.Nodes != "" && subInfo.Nodes != "null" {
			// nodes 是逗号分隔的字符串，如 "1,2,3"
			nodeIdStrs := strings.Split(subInfo.Nodes, ",")
			for _, idStr := range nodeIdStrs {
				idStr = strings.TrimSpace(idStr)
				if idStr != "" {
					var nodeId int64
					if _, err := fmt.Sscanf(idStr, "%d", &nodeId); err == nil {
						allDirectNodeIds = append(allDirectNodeIds, nodeId)
					}
				}
			}
			logger.Debugf("[PreviewUserNodes] subscribe_id=%d has direct nodes: %s", subInfo.Id, subInfo.Nodes)
		}
	}
	// 去重
	allDirectNodeIds = removeDuplicateInt64(allDirectNodeIds)
	logger.Infof("[PreviewUserNodes] collected direct node_ids: %v", allDirectNodeIds)

	// 4. 判断分组功能是否启用
	type SystemConfig struct {
		Value string
	}
	var config SystemConfig
	l.svcCtx.DB.Model(&struct {
		Category string `gorm:"column:category"`
		Key      string `gorm:"column:key"`
		Value    string `gorm:"column:value"`
	}{}).
		Table("system").
		Where("`category` = ? AND `key` = ?", "group", "enabled").
		Select("value").
		Scan(&config)

	logger.Infof("[PreviewUserNodes] groupEnabled: %v", config.Value)

	isGroupEnabled := config.Value == "true" || config.Value == "1"

	var filteredNodes []node.Node

	if isGroupEnabled {
		// === 启用分组功能：通过用户订阅的 node_group_id 查询节点 ===
		logger.Infof("[PreviewUserNodes] using group-based node filtering")

		if len(allNodeGroupIds) == 0 && len(allDirectNodeIds) == 0 {
			logger.Infof("[PreviewUserNodes] no node groups and no direct nodes found in user subscribes")
			resp = &types.PreviewUserNodesResponse{
				UserId:     req.UserId,
				NodeGroups: []types.NodeGroupItem{},
			}
			return resp, nil
		}

		// 5. 查询所有启用的节点（只有当有节点组时才查询）
		if len(allNodeGroupIds) > 0 {
			var dbNodes []node.Node
			err = l.svcCtx.DB.Model(&node.Node{}).
				Where("enabled = ? AND is_hidden = ?", true, false).
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
		}

	} else {
		// === 未启用分组功能：通过订阅的 node_tags 查询节点 ===
		logger.Infof("[PreviewUserNodes] using tag-based node filtering")

		// 从已查询的 subscribeInfos 中获取 node_tags
		var allTags []string
		for _, subInfo := range subscribeInfos {
			if subInfo.NodeTags != "" {
				tags := strings.Split(subInfo.NodeTags, ",")
				allTags = append(allTags, tags...)
			}
		}
		// 去重
		allTags = tool.RemoveDuplicateElements(allTags...)
		// 去除空字符串
		allTags = tool.RemoveStringElement(allTags, "")

		logger.Infof("[PreviewUserNodes] merged tags from subscribes: %v", allTags)

		if len(allTags) == 0 && len(allDirectNodeIds) == 0 {
			logger.Infof("[PreviewUserNodes] no tags and no direct nodes found in subscribes")
			resp = &types.PreviewUserNodesResponse{
				UserId:     req.UserId,
				NodeGroups: []types.NodeGroupItem{},
			}
			return resp, nil
		}

		// 8. 查询所有启用的节点（只有当有 tags 时才查询）
		if len(allTags) > 0 {
			var dbNodes []node.Node
			err = l.svcCtx.DB.Model(&node.Node{}).
				Where("enabled = ? AND is_hidden = ?", true, false).
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
	}

	// 10. 根据是否启用分组功能，选择不同的分组方式
	nodeGroupItems := make([]types.NodeGroupItem, 0)

	if isGroupEnabled {
		// === 启用分组：按节点组分组 ===
		// 转换为 types.Node 并按节点组分组
		type NodeWithGroup struct {
			Node         node.Node
			NodeGroupIds []int64
		}

		nodesWithGroup := make([]NodeWithGroup, 0, len(filteredNodes))
		for _, n := range filteredNodes {
			nodesWithGroup = append(nodesWithGroup, NodeWithGroup{
				Node:         n,
				NodeGroupIds: n.NodeGroupIds,
			})
		}

		// 按节点组分组节点
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
				// 如果节点属于节点组，按第一个节点组分组
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
					NodeGroupIds: tool.Int64SliceToStringSlice([]int64(ng.Node.NodeGroupIds)),
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
					NodeGroupIds: tool.Int64SliceToStringSlice([]int64(ng.Node.NodeGroupIds)),
					CreatedAt:    ng.Node.CreatedAt.Unix(),
					UpdatedAt:    ng.Node.UpdatedAt.Unix(),
				}

				groupMap[0].Nodes = append(groupMap[0].Nodes, node)
			}
		}

		// 查询节点组信息并构建响应
		nodeGroupInfoMap := make(map[int64]string)
		validGroupIds := make([]int64, 0)

		if len(allGroupIds) > 0 {
			type NodeGroupInfo struct {
				Id   int64
				Name string
			}
			var nodeGroupInfos []NodeGroupInfo
			err = l.svcCtx.DB.Model(&group.NodeGroup{}).
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

			// 记录无效的节点组ID
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

		// 构建响应：根据有效节点组ID重新分组节点
		publicNodes := make([]types.Node, 0)

		// 遍历所有分组，重新分类节点
		for groupId, gm := range groupMap {
			if groupId == 0 {
				// 本来就是无节点组的节点
				publicNodes = append(publicNodes, gm.Nodes...)
				continue
			}

			// 检查这个节点组ID是否有效
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

		// 添加公共节点组（如果有）
		if len(publicNodes) > 0 {
			nodeGroupItems = append(nodeGroupItems, types.NodeGroupItem{
				Id:    0,
				Name:  "",
				Nodes: publicNodes,
			})
			logger.Infof("[PreviewUserNodes] adding public group: nodes=%d", len(publicNodes))
		}

	} else {
		// === 未启用分组：按 tag 分组 ===
		// 按 tag 分组节点
		tagGroupMap := make(map[string][]types.Node)

		for _, n := range filteredNodes {
			tags := []string{}
			if n.Tags != "" {
				tags = strings.Split(n.Tags, ",")
			}

			// 转换节点
			node := types.Node{
				Id:           n.Id,
				Name:         n.Name,
				Tags:         tags,
				Port:         n.Port,
				Address:      n.Address,
				ServerId:     n.ServerId,
				Protocol:     n.Protocol,
				Enabled:      n.Enabled,
				Sort:         n.Sort,
				NodeGroupIds: tool.Int64SliceToStringSlice([]int64(n.NodeGroupIds)),
				CreatedAt:    n.CreatedAt.Unix(),
				UpdatedAt:    n.UpdatedAt.Unix(),
			}

			// 将节点添加到每个匹配的 tag 分组中
			if len(tags) > 0 {
				for _, tag := range tags {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						tagGroupMap[tag] = append(tagGroupMap[tag], node)
					}
				}
			} else {
				// 没有 tag 的节点放入特殊分组
				tagGroupMap[""] = append(tagGroupMap[""], node)
			}
		}

		// 构建响应：按 tag 分组
		for tag, nodes := range tagGroupMap {
			nodeGroupItems = append(nodeGroupItems, types.NodeGroupItem{
				Id:    0, // tag 分组使用 ID 0
				Name:  tag,
				Nodes: nodes,
			})
			logger.Infof("[PreviewUserNodes] adding tag group: tag=%s, nodes=%d", tag, len(nodes))
		}
	}

	// 添加套餐节点组（直接分配的节点）
	if len(allDirectNodeIds) > 0 {
		// 查询直接分配的节点详情
		var directNodes []node.Node
		err = l.svcCtx.DB.Model(&node.Node{}).
			Where("id IN ? AND enabled = ? AND is_hidden = ?", allDirectNodeIds, true, false).
			Find(&directNodes).Error
		if err != nil {
			logger.Errorf("[PreviewUserNodes] failed to get direct nodes: %v", err)
			return nil, err
		}

		if len(directNodes) > 0 {
			// 转换为 types.Node
			directNodeItems := make([]types.Node, 0, len(directNodes))
			for _, n := range directNodes {
				tags := []string{}
				if n.Tags != "" {
					tags = strings.Split(n.Tags, ",")
				}
				directNodeItems = append(directNodeItems, types.Node{
					Id:           n.Id,
					Name:         n.Name,
					Tags:         tags,
					Port:         n.Port,
					Address:      n.Address,
					ServerId:     n.ServerId,
					Protocol:     n.Protocol,
					Enabled:      n.Enabled,
					Sort:         n.Sort,
					NodeGroupIds: tool.Int64SliceToStringSlice([]int64(n.NodeGroupIds)),
					CreatedAt:    n.CreatedAt.Unix(),
					UpdatedAt:    n.UpdatedAt.Unix(),
				})
			}

			// 添加套餐节点组（使用特殊ID -1，Name 为空字符串，前端根据 ID -1 进行国际化）
			nodeGroupItems = append(nodeGroupItems, types.NodeGroupItem{
				Id:    -1,
				Name:  "", // 空字符串，前端根据 ID -1 识别并国际化
				Nodes: directNodeItems,
			})
			logger.Infof("[PreviewUserNodes] adding subscription nodes group: nodes=%d", len(directNodeItems))
		}
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
