package group

import (
	"context"
	"encoding/json"
	"time"

	modelgroup "github.com/perfect-panel/server/internal/model/group"
	modelsubscribe "github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type RecalculateGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Recalculate group
func NewRecalculateGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecalculateGroupLogic {
	return &RecalculateGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RecalculateGroupLogic) RecalculateGroup(req *types.RecalculateGroupRequest) error {
	// 验证 mode 参数
	if req.Mode != "average" && req.Mode != "subscribe" && req.Mode != "traffic" {
		return errors.New("invalid mode, must be one of: average, subscribe, traffic")
	}

	// 创建 GroupHistory 记录（state=pending）
	triggerType := req.TriggerType
	if triggerType == "" {
		triggerType = "manual" // 默认为手动触发
	}

	now := time.Now()
	var history *modelgroup.GroupHistory

	// 使用 Store Transaction 执行分组重算
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// 创建历史记录
		var err error
		history, err = store.Group().CreateGroupHistory(l.ctx, &modelgroup.GroupHistory{
			GroupMode:    req.Mode,
			TriggerType:  triggerType,
			State:        "pending",
			TotalUsers:   0,
			SuccessCount: 0,
			FailedCount:  0,
			StartTime:    &now,
		})
		if err != nil {
			l.Logger.Errorw("failed to create group history", zap.Any("error", err.Error()))
			return err
		}

		// 更新状态为 running
		if err := store.Group().UpdateGroupHistoryRunning(l.ctx, history.Id); err != nil {
			l.Logger.Errorw("failed to update history state to running", zap.Any("error", err.Error()))
			return err
		}

		// 根据 mode 执行不同的分组算法
		var affectedCount int

		switch req.Mode {
		case "average":
			affectedCount, err = l.executeAverageGrouping(store, history.Id)
			if err != nil {
				l.Logger.Errorw("failed to execute average grouping", zap.Any("error", err.Error()))
				return err
			}
		case "subscribe":
			affectedCount, err = l.executeSubscribeGrouping(store, history.Id)
			if err != nil {
				l.Logger.Errorw("failed to execute subscribe grouping", zap.Any("error", err.Error()))
				return err
			}
		case "traffic":
			affectedCount, err = l.executeTrafficGrouping(store, history.Id)
			if err != nil {
				l.Logger.Errorw("failed to execute traffic grouping", zap.Any("error", err.Error()))
				return err
			}
		}

		// 更新 GroupHistory 记录（state=completed, 统计成功/失败数）
		endTime := time.Now()
		if err := store.Group().CompleteGroupHistory(l.ctx, history.Id, affectedCount, affectedCount, 0, endTime); err != nil {
			l.Logger.Errorw("failed to update history state to completed", zap.Any("error", err.Error()))
			return err
		}

		l.Logger.Infof("group recalculation completed: mode=%s, affected_users=%d", req.Mode, affectedCount)
		return nil
	})

	if err != nil {
		// 如果失败，更新历史记录状态为 failed
		if history != nil {
			updateErr := l.svcCtx.Store.Group().FailGroupHistory(l.ctx, history.Id, err.Error(), time.Now())
			if updateErr != nil {
				l.Logger.Errorw("failed to update history state to failed", zap.Any("error", updateErr.Error()))
			}
		}
		return err
	}

	return nil
}

// getUserEmail 查询用户的邮箱
func (l *RecalculateGroupLogic) getUserEmail(store repository.Store, userId int64) string {
	authMethod, err := store.User().FindFirstUserAuthMethodByTypes(l.ctx, userId, "email", "6")
	if err != nil {
		return ""
	}

	return authMethod.AuthIdentifier
}

// executeAverageGrouping 实现平均分组算法（随机分配节点组到用户订阅）
// 新逻辑：获取所有有效用户订阅，从订阅的节点组ID中随机选择一个，设置到用户订阅的 node_group_id 字段
func (l *RecalculateGroupLogic) executeAverageGrouping(store repository.Store, historyId int64) (int, error) {
	// 1. 查询所有有效且未锁定的用户订阅（status IN (0, 1)）
	userSubscribes, err := store.User().FindUnlockedUserSubscribesByStatus(l.ctx, 0, 1)
	if err != nil {
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Logger.Infof("average grouping: no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Logger.Infof("average grouping: found %d valid and unlocked user subscribes", len(userSubscribes))

	// 1.5 查询所有参与计算的节点组ID
	calculationNodeGroups, err := store.Group().QueryCalculationNodeGroups(l.ctx)
	if err != nil {
		l.Logger.Errorw("failed to query calculation node groups", zap.Any("error", err.Error()))
		return 0, err
	}

	// 创建参与计算的节点组ID集合（用于快速查找）
	calculationNodeGroupIds := make(map[int64]bool)
	for _, ng := range calculationNodeGroups {
		calculationNodeGroupIds[ng.Id] = true
	}

	l.Logger.Infof("average grouping: found %d node groups with for_calculation=true", len(calculationNodeGroupIds))

	// 2. 批量查询订阅的节点组ID信息
	subscribeIds := make([]int64, len(userSubscribes))
	for i, us := range userSubscribes {
		subscribeIds[i] = us.SubscribeId
	}

	subscribeInfos, err := store.Subscribe().FindByIds(l.ctx, subscribeIds)
	if err != nil {
		l.Logger.Errorw("failed to query subscribe infos", zap.Any("error", err.Error()))
		return 0, err
	}

	// 创建 subscribe_id -> SubscribeInfo 的映射
	subInfoMap := make(map[int64]*modelsubscribe.Subscribe)
	for _, si := range subscribeInfos {
		subInfoMap[si.Id] = si
	}

	// 用于存储统计信息（按节点组ID统计用户数）
	groupUsersMap := make(map[int64][]struct {
		Id    int64  `json:"id"`
		Email string `json:"email"`
	})
	nodeGroupUserCount := make(map[int64]int) // node_group_id -> user_count
	nodeGroupNodeCount := make(map[int64]int) // node_group_id -> node_count

	// 3. 遍历所有用户订阅，按序平均分配节点组
	affectedCount := 0
	failedCount := 0

	// 为每个订阅维护一个分配索引，用于按序循环分配
	subscribeAllocationIndex := make(map[int64]int) // subscribe_id -> current_index

	for _, us := range userSubscribes {
		subInfo, ok := subInfoMap[us.SubscribeId]
		if !ok {
			l.Logger.Infow("subscribe not found",
				zap.Any("user_subscribe_id", us.Id),
				zap.Any("subscribe_id", us.SubscribeId))
			failedCount++
			continue
		}

		// 解析订阅的节点组ID列表，并过滤出参与计算的节点组
		var nodeGroupIds []int64
		if len(subInfo.NodeGroupIds) > 0 {
			// 只保留参与计算的节点组
			for _, ngId := range subInfo.NodeGroupIds {
				if calculationNodeGroupIds[ngId] {
					nodeGroupIds = append(nodeGroupIds, ngId)
				}
			}

			if len(nodeGroupIds) == 0 && len(subInfo.NodeGroupIds) > 0 {
				l.Logger.Debugw("all node_group_ids are not for calculation, setting to 0",
					zap.Any("subscribe_id", subInfo.Id),
					zap.Any("total_node_groups", len(subInfo.NodeGroupIds)))
			}
		}

		// 如果没有节点组ID,跳过
		if len(nodeGroupIds) == 0 {
			l.Logger.Debugf("no valid node_group_ids for subscribe_id=%d, setting to 0", subInfo.Id)
			if err := store.User().UpdateUserSubscribeNodeGroup(l.ctx, us.Id, 0); err != nil {
				l.Logger.Errorw("failed to update user_subscribe node_group_id",
					zap.Any("user_subscribe_id", us.Id),
					zap.Any("error", err.Error()))
				failedCount++
				continue
			}
		}

		// 按序选择节点组ID（循环轮询分配）
		selectedNodeGroupId := int64(0)
		if len(nodeGroupIds) > 0 {
			// 获取当前订阅的分配索引
			currentIndex := subscribeAllocationIndex[us.SubscribeId]
			// 选择当前索引对应的节点组
			selectedNodeGroupId = nodeGroupIds[currentIndex]
			// 更新索引，循环使用（轮询）
			subscribeAllocationIndex[us.SubscribeId] = (currentIndex + 1) % len(nodeGroupIds)

			l.Logger.Debugf("assigning user_subscribe_id=%d (subscribe_id=%d) to node_group_id=%d (index=%d, total_options=%d, mode=sequential)",
				us.Id, us.SubscribeId, selectedNodeGroupId, currentIndex, len(nodeGroupIds))
		}

		// 更新 user_subscribe 的 node_group_id 字段（单个ID）
		if err := store.User().UpdateUserSubscribeNodeGroup(l.ctx, us.Id, selectedNodeGroupId); err != nil {
			l.Logger.Errorw("failed to update user_subscribe node_group_id",
				zap.Any("user_subscribe_id", us.Id),
				zap.Any("error", err.Error()))
			failedCount++
			continue
		}

		// 只统计有节点组的用户
		if selectedNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(store, us.UserId)
			groupUsersMap[selectedNodeGroupId] = append(groupUsersMap[selectedNodeGroupId], struct {
				Id    int64  `json:"id"`
				Email string `json:"email"`
			}{
				Id:    us.UserId,
				Email: email,
			})
			nodeGroupUserCount[selectedNodeGroupId]++
		}

		affectedCount++
	}

	l.Logger.Infof("average grouping completed: affected=%d, failed=%d", affectedCount, failedCount)

	// 4. 创建分组历史详情记录（按节点组ID统计）
	for nodeGroupId, users := range groupUsersMap {
		userCount := len(users)
		if userCount == 0 {
			continue
		}

		// 统计该节点组的节点数
		nodeCount := 0
		if nodeGroupId > 0 {
			var err error
			nodeCount64, err := store.Group().CountNodesByNodeGroupId(l.ctx, nodeGroupId)
			if err != nil {
				l.Logger.Errorw("failed to count nodes",
					zap.Any("node_group_id", nodeGroupId),
					zap.Any("error", err.Error()))
			}
			nodeCount = int(nodeCount64)
		}
		nodeGroupNodeCount[nodeGroupId] = int(nodeCount)

		// 序列化用户信息为 JSON
		userDataJSON := "[]"
		if jsonData, err := json.Marshal(users); err == nil {
			userDataJSON = string(jsonData)
		} else {
			l.Logger.Errorw("failed to marshal user data",
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
		}

		// 创建历史详情（使用 node_group_id 作为分组标识）
		if err := store.Group().CreateGroupHistoryDetail(l.ctx, &modelgroup.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   nodeCount,
			UserData:    userDataJSON,
		}); err != nil {
			l.Logger.Errorw("failed to create group history detail",
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
		}

		l.Logger.Infof("Average Group (node_group_id=%d): users=%d, nodes=%d",
			nodeGroupId, userCount, nodeCount)
	}

	return affectedCount, nil
}

// executeSubscribeGrouping 实现基于订阅套餐的分组算法
// 逻辑：查询有效订阅 → 获取订阅的 node_group_ids → 取第一个 node_group_id（如果有） → 更新 user_subscribe.node_group_id
// 订阅过期的用户 → 设置 node_group_id 为 0
func (l *RecalculateGroupLogic) executeSubscribeGrouping(store repository.Store, historyId int64) (int, error) {
	// 1. 查询所有有效且未锁定的用户订阅（status IN (0, 1), group_locked = 0）
	userSubscribes, err := store.User().FindUnlockedUserSubscribesByStatus(l.ctx, 0, 1)
	if err != nil {
		l.Logger.Errorw("failed to query user subscribes", zap.Any("error", err.Error()))
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Logger.Infof("subscribe grouping: no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Logger.Infof("subscribe grouping: found %d valid and unlocked user subscribes", len(userSubscribes))

	// 1.5 查询所有参与计算的节点组ID
	calculationNodeGroups, err := store.Group().QueryCalculationNodeGroups(l.ctx)
	if err != nil {
		l.Logger.Errorw("failed to query calculation node groups", zap.Any("error", err.Error()))
		return 0, err
	}

	// 创建参与计算的节点组ID集合（用于快速查找）
	calculationNodeGroupIds := make(map[int64]bool)
	for _, ng := range calculationNodeGroups {
		calculationNodeGroupIds[ng.Id] = true
	}

	l.Logger.Infof("subscribe grouping: found %d node groups with for_calculation=true", len(calculationNodeGroupIds))

	// 2. 批量查询订阅的节点组ID信息
	subscribeIds := make([]int64, len(userSubscribes))
	for i, us := range userSubscribes {
		subscribeIds[i] = us.SubscribeId
	}

	subscribeInfos, err := store.Subscribe().FindByIds(l.ctx, subscribeIds)
	if err != nil {
		l.Logger.Errorw("failed to query subscribe infos", zap.Any("error", err.Error()))
		return 0, err
	}

	// 创建 subscribe_id -> SubscribeInfo 的映射
	subInfoMap := make(map[int64]*modelsubscribe.Subscribe)
	for _, si := range subscribeInfos {
		subInfoMap[si.Id] = si
	}

	// 用于存储统计信息（按节点组ID统计用户数）
	type UserInfo struct {
		Id    int64  `json:"id"`
		Email string `json:"email"`
	}
	groupUsersMap := make(map[int64][]UserInfo)
	nodeGroupUserCount := make(map[int64]int) // node_group_id -> user_count
	nodeGroupNodeCount := make(map[int64]int) // node_group_id -> node_count

	// 3. 遍历所有用户订阅，取第一个节点组ID
	affectedCount := 0
	failedCount := 0

	for _, us := range userSubscribes {
		subInfo, ok := subInfoMap[us.SubscribeId]
		if !ok {
			l.Logger.Infow("subscribe not found",
				zap.Any("user_subscribe_id", us.Id),
				zap.Any("subscribe_id", us.SubscribeId))
			failedCount++
			continue
		}

		// 解析订阅的节点组ID列表，并过滤出参与计算的节点组
		var nodeGroupIds []int64
		if len(subInfo.NodeGroupIds) > 0 {
			// 只保留参与计算的节点组
			for _, ngId := range subInfo.NodeGroupIds {
				if calculationNodeGroupIds[ngId] {
					nodeGroupIds = append(nodeGroupIds, ngId)
				}
			}

			if len(nodeGroupIds) == 0 && len(subInfo.NodeGroupIds) > 0 {
				l.Logger.Debugw("all node_group_ids are not for calculation, setting to 0",
					zap.Any("subscribe_id", subInfo.Id),
					zap.Any("total_node_groups", len(subInfo.NodeGroupIds)))
			}
		}

		// 取第一个参与计算的节点组ID（如果有），否则设置为 0
		selectedNodeGroupId := int64(0)
		if len(nodeGroupIds) > 0 {
			selectedNodeGroupId = nodeGroupIds[0]
		}

		l.Logger.Debugf("assigning user_subscribe_id=%d (subscribe_id=%d) to node_group_id=%d (total_options=%d, selected_first)",
			us.Id, us.SubscribeId, selectedNodeGroupId, len(nodeGroupIds))

		// 更新 user_subscribe 的 node_group_id 字段
		if err := store.User().UpdateUserSubscribeNodeGroup(l.ctx, us.Id, selectedNodeGroupId); err != nil {
			l.Logger.Errorw("failed to update user_subscribe node_group_id",
				zap.Any("user_subscribe_id", us.Id),
				zap.Any("error", err.Error()))
			failedCount++
			continue
		}

		// 只统计有节点组的用户
		if selectedNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(store, us.UserId)
			groupUsersMap[selectedNodeGroupId] = append(groupUsersMap[selectedNodeGroupId], UserInfo{
				Id:    us.UserId,
				Email: email,
			})
			nodeGroupUserCount[selectedNodeGroupId]++
		}

		affectedCount++
	}

	l.Logger.Infof("subscribe grouping completed: affected=%d, failed=%d", affectedCount, failedCount)

	// 4. 处理订阅过期/失效的用户，设置 node_group_id 为 0
	// 查询所有没有有效订阅且未锁定的用户订阅记录
	expiredUserSubscribes, err := store.User().FindUnlockedUserSubscribesStatusNotIn(l.ctx, 0, 1)
	if err != nil {
		l.Logger.Errorw("failed to query expired user subscribes", zap.Any("error", err.Error()))
		// 继续处理，不因为过期用户查询失败而影响
	} else {
		l.Logger.Infof("found %d expired user subscribes for subscribe-based grouping, will set node_group_id to 0", len(expiredUserSubscribes))

		expiredAffectedCount := 0
		for _, eu := range expiredUserSubscribes {
			// 更新 user_subscribe 表的 node_group_id 字段到 0
			if err := store.User().UpdateUserSubscribeNodeGroup(l.ctx, eu.Id, 0); err != nil {
				l.Logger.Errorw("failed to update expired user subscribe node_group_id",
					zap.Any("user_subscribe_id", eu.Id),
					zap.Any("error", err.Error()))
				continue
			}

			expiredAffectedCount++
		}

		l.Logger.Infof("expired user subscribes grouping completed: affected=%d", expiredAffectedCount)
	}

	// 5. 创建分组历史详情记录（按节点组ID统计）
	for nodeGroupId, users := range groupUsersMap {
		userCount := len(users)
		if userCount == 0 {
			continue
		}

		// 统计该节点组的节点数
		nodeCount := 0
		if nodeGroupId > 0 {
			nodeCount64, err := store.Group().CountNodesByNodeGroupId(l.ctx, nodeGroupId)
			if err != nil {
				l.Logger.Errorw("failed to count nodes",
					zap.Any("node_group_id", nodeGroupId),
					zap.Any("error", err.Error()))
			}
			nodeCount = int(nodeCount64)
		}
		nodeGroupNodeCount[nodeGroupId] = int(nodeCount)

		// 序列化用户信息为 JSON
		userDataJSON := "[]"
		if jsonData, err := json.Marshal(users); err == nil {
			userDataJSON = string(jsonData)
		} else {
			l.Logger.Errorw("failed to marshal user data",
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
		}

		// 创建历史详情
		if err := store.Group().CreateGroupHistoryDetail(l.ctx, &modelgroup.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   nodeCount,
			UserData:    userDataJSON,
		}); err != nil {
			l.Logger.Errorw("failed to create group history detail",
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
		}

		l.Logger.Infof("Subscribe Group (node_group_id=%d): users=%d, nodes=%d",
			nodeGroupId, userCount, nodeCount)
	}

	return affectedCount, nil
}

// executeTrafficGrouping 实现基于流量的分组算法
// 逻辑：根据配置的流量范围，将用户分配到对应的用户组
func (l *RecalculateGroupLogic) executeTrafficGrouping(store repository.Store, historyId int64) (int, error) {
	// 用于存储每个节点组的用户信息（id 和 email）
	type UserInfo struct {
		Id    int64  `json:"id"`
		Email string `json:"email"`
	}
	groupUsersMap := make(map[int64][]UserInfo) // node_group_id -> []UserInfo

	// 1. 获取所有设置了流量区间的节点组
	nodeGroups, err := store.Group().QueryPositiveTrafficCalculationNodeGroups(l.ctx)
	if err != nil {
		l.Logger.Errorw("failed to query node groups", zap.Any("error", err.Error()))
		return 0, err
	}

	if len(nodeGroups) == 0 {
		l.Logger.Infow("no node groups with traffic ranges configured")
		return 0, nil
	}

	l.Logger.Infow("executeTrafficGrouping loaded node groups",
		zap.Any("node_groups_count", len(nodeGroups)))

	// 2. 查询所有有效且未锁定的用户订阅及其已用流量
	userSubscribes, err := store.User().FindUnlockedUserSubscribesByStatus(l.ctx, 0, 1)
	if err != nil {
		l.Logger.Errorw("failed to query user subscribes", zap.Any("error", err.Error()))
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Logger.Infow("no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Logger.Infow("found user subscribes for traffic-based grouping", zap.Any("count", len(userSubscribes)))

	// 3. 根据流量范围分配节点组ID到用户订阅
	affectedCount := 0
	groupUserCount := make(map[int64]int) // node_group_id -> user_count

	for _, us := range userSubscribes {
		// 将字节转换为 GB
		usedTrafficGB := float64(us.Upload+us.Download) / (1024 * 1024 * 1024)

		// 查找匹配的流量范围（使用左闭右开区间 [Min, Max)）
		var targetNodeGroupId int64 = 0
		for _, ng := range nodeGroups {
			if ng.MinTrafficGB == nil || ng.MaxTrafficGB == nil {
				continue
			}
			minTraffic := float64(*ng.MinTrafficGB)
			maxTraffic := float64(*ng.MaxTrafficGB)

			// 检查是否在区间内 [min, max)
			if usedTrafficGB >= minTraffic && usedTrafficGB < maxTraffic {
				targetNodeGroupId = ng.Id
				break
			}
		}

		// 如果没有匹配到任何范围，targetNodeGroupId 保持为 0（不分配节点组）

		// 更新 user_subscribe 的 node_group_id 字段
		if err := store.User().UpdateUserSubscribeNodeGroup(l.ctx, us.Id, targetNodeGroupId); err != nil {
			l.Logger.Errorw("failed to update user subscribe node_group_id",
				zap.Any("user_subscribe_id", us.Id),
				zap.Any("target_node_group_id", targetNodeGroupId),
				zap.Any("error", err.Error()))
			continue
		}

		// 只有分配了节点组的用户才记录到历史
		if targetNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(store, us.UserId)
			userInfo := UserInfo{
				Id:    us.UserId,
				Email: email,
			}
			groupUsersMap[targetNodeGroupId] = append(groupUsersMap[targetNodeGroupId], userInfo)
			groupUserCount[targetNodeGroupId]++

			l.Logger.Debugf("assigned user subscribe %d (traffic: %.2fGB) to node group %d",
				us.Id, usedTrafficGB, targetNodeGroupId)
		} else {
			l.Logger.Debugf("user subscribe %d (traffic: %.2fGB) not assigned to any node group",
				us.Id, usedTrafficGB)
		}

		affectedCount++
	}

	l.Logger.Infof("traffic-based grouping completed: affected_subscribes=%d", affectedCount)

	// 4. 创建分组历史详情记录（只统计有用户的节点组）
	nodeGroupCount := make(map[int64]int) // node_group_id -> node_count
	for _, ng := range nodeGroups {
		nodeGroupCount[ng.Id] = 1 // 每个节点组计为1
	}

	for nodeGroupId, userCount := range groupUserCount {
		userDataJSON, err := json.Marshal(groupUsersMap[nodeGroupId])
		if err != nil {
			l.Logger.Errorw("failed to marshal user data",
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
			continue
		}

		if err := store.Group().CreateGroupHistoryDetail(l.ctx, &modelgroup.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   nodeGroupCount[nodeGroupId],
			UserData:    string(userDataJSON),
		}); err != nil {
			l.Logger.Errorw("failed to create group history detail",
				zap.Any("history_id", historyId),
				zap.Any("node_group_id", nodeGroupId),
				zap.Any("error", err.Error()))
		}
	}

	return affectedCount, nil
}

// containsIgnoreCase checks if a string contains another substring (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	// Simple case-insensitive contains check
	sLower := toLower(s)
	substrLower := toLower(substr)

	return contains(sLower, substrLower)
}

// toLower converts a string to lowercase
func toLower(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + ('a' - 'A')
		} else {
			result[i] = r
		}
	}
	return string(result)
}

// contains checks if a string contains another substring (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

// indexOf returns the index of the first occurrence of substr in s, or -1 if not found
func indexOf(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	if n > len(s) {
		return -1
	}

	// Simple string search
	for i := 0; i <= len(s)-n; i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}
