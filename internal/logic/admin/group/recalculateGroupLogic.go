package group

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type RecalculateGroupLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Recalculate group
func NewRecalculateGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecalculateGroupLogic {
	return &RecalculateGroupLogic{
		Logger: logger.WithContext(ctx),
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

	history := &group.GroupHistory{
		GroupMode:    req.Mode,
		TriggerType:  triggerType,
		TotalUsers:   0,
		SuccessCount: 0,
		FailedCount:  0,
	}
	now := time.Now()
	history.StartTime = &now

	// 使用 GORM Transaction 执行分组重算
	err := l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 创建历史记录
		if err := tx.Create(history).Error; err != nil {
			l.Errorw("failed to create group history", logger.Field("error", err.Error()))
			return err
		}

		// 更新状态为 running
		if err := tx.Model(history).Update("state", "running").Error; err != nil {
			l.Errorw("failed to update history state to running", logger.Field("error", err.Error()))
			return err
		}

		// 根据 mode 执行不同的分组算法
		var affectedCount int
		var err error

		switch req.Mode {
		case "average":
			affectedCount, err = l.executeAverageGrouping(tx, history.Id)
			if err != nil {
				l.Errorw("failed to execute average grouping", logger.Field("error", err.Error()))
				return err
			}
		case "subscribe":
			affectedCount, err = l.executeSubscribeGrouping(tx, history.Id)
			if err != nil {
				l.Errorw("failed to execute subscribe grouping", logger.Field("error", err.Error()))
				return err
			}
		case "traffic":
			affectedCount, err = l.executeTrafficGrouping(tx, history.Id)
			if err != nil {
				l.Errorw("failed to execute traffic grouping", logger.Field("error", err.Error()))
				return err
			}
		}

		// 更新 GroupHistory 记录（state=completed, 统计成功/失败数）
		endTime := time.Now()
		updates := map[string]interface{}{
			"state":         "completed",
			"total_users":   affectedCount,
			"success_count": affectedCount, // 暂时假设所有都成功
			"failed_count":  0,
			"end_time":      endTime,
		}

		if err := tx.Model(history).Updates(updates).Error; err != nil {
			l.Errorw("failed to update history state to completed", logger.Field("error", err.Error()))
			return err
		}

		l.Infof("group recalculation completed: mode=%s, affected_users=%d", req.Mode, affectedCount)
		return nil
	})

	if err != nil {
		// 如果失败，更新历史记录状态为 failed
		updateErr := l.svcCtx.DB.Model(history).Updates(map[string]interface{}{
			"state":         "failed",
			"error_message": err.Error(),
			"end_time":      time.Now(),
		}).Error
		if updateErr != nil {
			l.Errorw("failed to update history state to failed", logger.Field("error", updateErr.Error()))
		}
		return err
	}

	return nil
}

// getUserEmail 查询用户的邮箱
func (l *RecalculateGroupLogic) getUserEmail(tx *gorm.DB, userId int64) string {
	type UserAuthMethod struct {
		AuthIdentifier string `json:"auth_identifier"`
	}

	var authMethod UserAuthMethod
	if err := tx.Table("user_auth_methods").
		Select("auth_identifier").
		Where("user_id = ? AND (auth_type = ? OR auth_type = ?)", userId, "email", "6").
		First(&authMethod).Error; err != nil {
		return ""
	}

	return authMethod.AuthIdentifier
}

// executeAverageGrouping 实现平均分组算法（随机分配节点组到用户订阅）
// 新逻辑：获取所有有效用户订阅，从订阅的节点组ID中随机选择一个，设置到用户订阅的 node_group_id 字段
func (l *RecalculateGroupLogic) executeAverageGrouping(tx *gorm.DB, historyId int64) (int, error) {
	// 1. 查询所有有效且未锁定的用户订阅（status IN (0, 1)）
	type UserSubscribeInfo struct {
		Id          int64 `json:"id"`
		UserId      int64 `json:"user_id"`
		SubscribeId int64 `json:"subscribe_id"`
	}

	var userSubscribes []UserSubscribeInfo
	if err := tx.Table("user_subscribe").
		Select("id, user_id, subscribe_id").
		Where("group_locked = ? AND status IN (0, 1)", 0). // 只查询未锁定且有效的用户订阅
		Scan(&userSubscribes).Error; err != nil {
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Infof("average grouping: no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Infof("average grouping: found %d valid and unlocked user subscribes", len(userSubscribes))

	// 1.5 查询所有参与计算的节点组ID
	var calculationNodeGroups []group.NodeGroup
	if err := tx.Table("node_group").
		Select("id").
		Where("for_calculation = ?", true).
		Scan(&calculationNodeGroups).Error; err != nil {
		l.Errorw("failed to query calculation node groups", logger.Field("error", err.Error()))
		return 0, err
	}

	// 创建参与计算的节点组ID集合（用于快速查找）
	calculationNodeGroupIds := make(map[int64]bool)
	for _, ng := range calculationNodeGroups {
		calculationNodeGroupIds[ng.Id] = true
	}

	l.Infof("average grouping: found %d node groups with for_calculation=true", len(calculationNodeGroupIds))

	// 2. 批量查询订阅的节点组ID信息
	subscribeIds := make([]int64, len(userSubscribes))
	for i, us := range userSubscribes {
		subscribeIds[i] = us.SubscribeId
	}

	type SubscribeInfo struct {
		Id           int64  `json:"id"`
		NodeGroupIds string `json:"node_group_ids"` // JSON string
	}
	var subscribeInfos []SubscribeInfo
	if err := tx.Table("subscribe").
		Select("id, node_group_ids").
		Where("id IN ?", subscribeIds).
		Find(&subscribeInfos).Error; err != nil {
		l.Errorw("failed to query subscribe infos", logger.Field("error", err.Error()))
		return 0, err
	}

	// 创建 subscribe_id -> SubscribeInfo 的映射
	subInfoMap := make(map[int64]SubscribeInfo)
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
			l.Infow("subscribe not found",
				logger.Field("user_subscribe_id", us.Id),
				logger.Field("subscribe_id", us.SubscribeId))
			failedCount++
			continue
		}

		// 解析订阅的节点组ID列表，并过滤出参与计算的节点组
		var nodeGroupIds []int64
		if subInfo.NodeGroupIds != "" && subInfo.NodeGroupIds != "[]" {
			var allNodeGroupIds []int64
			if err := json.Unmarshal([]byte(subInfo.NodeGroupIds), &allNodeGroupIds); err != nil {
				l.Errorw("failed to parse node_group_ids",
					logger.Field("subscribe_id", subInfo.Id),
					logger.Field("node_group_ids", subInfo.NodeGroupIds),
					logger.Field("error", err.Error()))
				failedCount++
				continue
			}

			// 只保留参与计算的节点组
			for _, ngId := range allNodeGroupIds {
				if calculationNodeGroupIds[ngId] {
					nodeGroupIds = append(nodeGroupIds, ngId)
				}
			}

			if len(nodeGroupIds) == 0 && len(allNodeGroupIds) > 0 {
				l.Debugw("all node_group_ids are not for calculation, setting to 0",
					logger.Field("subscribe_id", subInfo.Id),
					logger.Field("total_node_groups", len(allNodeGroupIds)))
			}
		}

		// 如果没有节点组ID，跳过
		if len(nodeGroupIds) == 0 {
			l.Debugf("no valid node_group_ids for subscribe_id=%d, setting to 0", subInfo.Id)
			if err := tx.Table("user_subscribe").
				Where("id = ?", us.Id).
				Update("node_group_id", 0).Error; err != nil {
				l.Errorw("failed to update user_subscribe node_group_id",
					logger.Field("user_subscribe_id", us.Id),
					logger.Field("error", err.Error()))
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

			l.Debugf("assigning user_subscribe_id=%d (subscribe_id=%d) to node_group_id=%d (index=%d, total_options=%d, mode=sequential)",
				us.Id, us.SubscribeId, selectedNodeGroupId, currentIndex, len(nodeGroupIds))
		}

		// 更新 user_subscribe 的 node_group_id 字段（单个ID）
		if err := tx.Table("user_subscribe").
			Where("id = ?", us.Id).
			Update("node_group_id", selectedNodeGroupId).Error; err != nil {
			l.Errorw("failed to update user_subscribe node_group_id",
				logger.Field("user_subscribe_id", us.Id),
				logger.Field("error", err.Error()))
			failedCount++
			continue
		}

		// 只统计有节点组的用户
		if selectedNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(tx, us.UserId)
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

	l.Infof("average grouping completed: affected=%d, failed=%d", affectedCount, failedCount)

	// 4. 创建分组历史详情记录（按节点组ID统计）
	for nodeGroupId, users := range groupUsersMap {
		userCount := len(users)
		if userCount == 0 {
			continue
		}

		// 统计该节点组的节点数
		var nodeCount int64 = 0
		if nodeGroupId > 0 {
			if err := tx.Table("nodes").
				Where("JSON_CONTAINS(node_group_ids, ?)", nodeGroupId).
				Count(&nodeCount).Error; err != nil {
				l.Errorw("failed to count nodes",
					logger.Field("node_group_id", nodeGroupId),
					logger.Field("error", err.Error()))
			}
		}
		nodeGroupNodeCount[nodeGroupId] = int(nodeCount)

		// 序列化用户信息为 JSON
		userDataJSON := "[]"
		if jsonData, err := json.Marshal(users); err == nil {
			userDataJSON = string(jsonData)
		} else {
			l.Errorw("failed to marshal user data",
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
		}

		// 创建历史详情（使用 node_group_id 作为分组标识）
		detail := &group.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   int(nodeCount),
			UserData:    userDataJSON,
		}

		if err := tx.Create(detail).Error; err != nil {
			l.Errorw("failed to create group history detail",
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
		}

		l.Infof("Average Group (node_group_id=%d): users=%d, nodes=%d",
			nodeGroupId, userCount, nodeCount)
	}

	return affectedCount, nil
}

// executeSubscribeGrouping 实现基于订阅套餐的分组算法
// 逻辑：查询有效订阅 → 获取订阅的 node_group_ids → 取第一个 node_group_id（如果有） → 更新 user_subscribe.node_group_id
// 订阅过期的用户 → 设置 node_group_id 为 0
func (l *RecalculateGroupLogic) executeSubscribeGrouping(tx *gorm.DB, historyId int64) (int, error) {
	// 1. 查询所有有效且未锁定的用户订阅（status IN (0, 1), group_locked = 0）
	type UserSubscribeInfo struct {
		Id          int64 `json:"id"`
		UserId      int64 `json:"user_id"`
		SubscribeId int64 `json:"subscribe_id"`
	}

	var userSubscribes []UserSubscribeInfo
	if err := tx.Table("user_subscribe").
		Select("id, user_id, subscribe_id").
		Where("group_locked = ? AND status IN (0, 1)", 0).
		Scan(&userSubscribes).Error; err != nil {
		l.Errorw("failed to query user subscribes", logger.Field("error", err.Error()))
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Infof("subscribe grouping: no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Infof("subscribe grouping: found %d valid and unlocked user subscribes", len(userSubscribes))

	// 1.5 查询所有参与计算的节点组ID
	var calculationNodeGroups []group.NodeGroup
	if err := tx.Table("node_group").
		Select("id").
		Where("for_calculation = ?", true).
		Scan(&calculationNodeGroups).Error; err != nil {
		l.Errorw("failed to query calculation node groups", logger.Field("error", err.Error()))
		return 0, err
	}

	// 创建参与计算的节点组ID集合（用于快速查找）
	calculationNodeGroupIds := make(map[int64]bool)
	for _, ng := range calculationNodeGroups {
		calculationNodeGroupIds[ng.Id] = true
	}

	l.Infof("subscribe grouping: found %d node groups with for_calculation=true", len(calculationNodeGroupIds))

	// 2. 批量查询订阅的节点组ID信息
	subscribeIds := make([]int64, len(userSubscribes))
	for i, us := range userSubscribes {
		subscribeIds[i] = us.SubscribeId
	}

	type SubscribeInfo struct {
		Id           int64  `json:"id"`
		NodeGroupIds string `json:"node_group_ids"` // JSON string
	}
	var subscribeInfos []SubscribeInfo
	if err := tx.Table("subscribe").
		Select("id, node_group_ids").
		Where("id IN ?", subscribeIds).
		Find(&subscribeInfos).Error; err != nil {
		l.Errorw("failed to query subscribe infos", logger.Field("error", err.Error()))
		return 0, err
	}

	// 创建 subscribe_id -> SubscribeInfo 的映射
	subInfoMap := make(map[int64]SubscribeInfo)
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
			l.Infow("subscribe not found",
				logger.Field("user_subscribe_id", us.Id),
				logger.Field("subscribe_id", us.SubscribeId))
			failedCount++
			continue
		}

		// 解析订阅的节点组ID列表，并过滤出参与计算的节点组
		var nodeGroupIds []int64
		if subInfo.NodeGroupIds != "" && subInfo.NodeGroupIds != "[]" {
			var allNodeGroupIds []int64
			if err := json.Unmarshal([]byte(subInfo.NodeGroupIds), &allNodeGroupIds); err != nil {
				l.Errorw("failed to parse node_group_ids",
					logger.Field("subscribe_id", subInfo.Id),
					logger.Field("node_group_ids", subInfo.NodeGroupIds),
					logger.Field("error", err.Error()))
				failedCount++
				continue
			}

			// 只保留参与计算的节点组
			for _, ngId := range allNodeGroupIds {
				if calculationNodeGroupIds[ngId] {
					nodeGroupIds = append(nodeGroupIds, ngId)
				}
			}

			if len(nodeGroupIds) == 0 && len(allNodeGroupIds) > 0 {
				l.Debugw("all node_group_ids are not for calculation, setting to 0",
					logger.Field("subscribe_id", subInfo.Id),
					logger.Field("total_node_groups", len(allNodeGroupIds)))
			}
		}

		// 取第一个参与计算的节点组ID（如果有），否则设置为 0
		selectedNodeGroupId := int64(0)
		if len(nodeGroupIds) > 0 {
			selectedNodeGroupId = nodeGroupIds[0]
		}

		l.Debugf("assigning user_subscribe_id=%d (subscribe_id=%d) to node_group_id=%d (total_options=%d, selected_first)",
			us.Id, us.SubscribeId, selectedNodeGroupId, len(nodeGroupIds))

		// 更新 user_subscribe 的 node_group_id 字段
		if err := tx.Table("user_subscribe").
			Where("id = ?", us.Id).
			Update("node_group_id", selectedNodeGroupId).Error; err != nil {
			l.Errorw("failed to update user_subscribe node_group_id",
				logger.Field("user_subscribe_id", us.Id),
				logger.Field("error", err.Error()))
			failedCount++
			continue
		}

		// 只统计有节点组的用户
		if selectedNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(tx, us.UserId)
			groupUsersMap[selectedNodeGroupId] = append(groupUsersMap[selectedNodeGroupId], UserInfo{
				Id:    us.UserId,
				Email: email,
			})
			nodeGroupUserCount[selectedNodeGroupId]++
		}

		affectedCount++
	}

	l.Infof("subscribe grouping completed: affected=%d, failed=%d", affectedCount, failedCount)

	// 4. 处理订阅过期/失效的用户，设置 node_group_id 为 0
	// 查询所有没有有效订阅且未锁定的用户订阅记录
	var expiredUserSubscribes []struct {
		Id     int64 `json:"id"`
		UserId int64 `json:"user_id"`
	}

	if err := tx.Raw(`
		SELECT us.id, us.user_id
		FROM user_subscribe as us
		WHERE us.group_locked = 0
			AND us.status NOT IN (0, 1)
	`).Scan(&expiredUserSubscribes).Error; err != nil {
		l.Errorw("failed to query expired user subscribes", logger.Field("error", err.Error()))
		// 继续处理，不因为过期用户查询失败而影响
	} else {
		l.Infof("found %d expired user subscribes for subscribe-based grouping, will set node_group_id to 0", len(expiredUserSubscribes))

		expiredAffectedCount := 0
		for _, eu := range expiredUserSubscribes {
			// 更新 user_subscribe 表的 node_group_id 字段到 0
			if err := tx.Table("user_subscribe").
				Where("id = ?", eu.Id).
				Update("node_group_id", 0).Error; err != nil {
				l.Errorw("failed to update expired user subscribe node_group_id",
					logger.Field("user_subscribe_id", eu.Id),
					logger.Field("error", err.Error()))
				continue
			}

			expiredAffectedCount++
		}

		l.Infof("expired user subscribes grouping completed: affected=%d", expiredAffectedCount)
	}

	// 5. 创建分组历史详情记录（按节点组ID统计）
	for nodeGroupId, users := range groupUsersMap {
		userCount := len(users)
		if userCount == 0 {
			continue
		}

		// 统计该节点组的节点数
		var nodeCount int64 = 0
		if nodeGroupId > 0 {
			if err := tx.Table("nodes").
				Where("JSON_CONTAINS(node_group_ids, ?)", nodeGroupId).
				Count(&nodeCount).Error; err != nil {
				l.Errorw("failed to count nodes",
					logger.Field("node_group_id", nodeGroupId),
					logger.Field("error", err.Error()))
			}
		}
		nodeGroupNodeCount[nodeGroupId] = int(nodeCount)

		// 序列化用户信息为 JSON
		userDataJSON := "[]"
		if jsonData, err := json.Marshal(users); err == nil {
			userDataJSON = string(jsonData)
		} else {
			l.Errorw("failed to marshal user data",
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
		}

		// 创建历史详情
		detail := &group.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   int(nodeCount),
			UserData:    userDataJSON,
		}

		if err := tx.Create(detail).Error; err != nil {
			l.Errorw("failed to create group history detail",
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
		}

		l.Infof("Subscribe Group (node_group_id=%d): users=%d, nodes=%d",
			nodeGroupId, userCount, nodeCount)
	}

	return affectedCount, nil
}

// executeTrafficGrouping 实现基于流量的分组算法
// 逻辑：根据配置的流量范围，将用户分配到对应的用户组
func (l *RecalculateGroupLogic) executeTrafficGrouping(tx *gorm.DB, historyId int64) (int, error) {
	// 用于存储每个节点组的用户信息（id 和 email）
	type UserInfo struct {
		Id    int64  `json:"id"`
		Email string `json:"email"`
	}
	groupUsersMap := make(map[int64][]UserInfo) // node_group_id -> []UserInfo

	// 1. 获取所有设置了流量区间的节点组
	var nodeGroups []group.NodeGroup
	if err := tx.Where("for_calculation = ?", true).
		Where("(min_traffic_gb > 0 OR max_traffic_gb > 0)").
		Find(&nodeGroups).Error; err != nil {
		l.Errorw("failed to query node groups", logger.Field("error", err.Error()))
		return 0, err
	}

	if len(nodeGroups) == 0 {
		l.Infow("no node groups with traffic ranges configured")
		return 0, nil
	}

	l.Infow("executeTrafficGrouping loaded node groups",
		logger.Field("node_groups_count", len(nodeGroups)))

	// 2. 查询所有有效且未锁定的用户订阅及其已用流量
	type UserSubscribeInfo struct {
		Id          int64
		UserId      int64
		Upload      int64
		Download    int64
		UsedTraffic int64 // 已用流量 = upload + download (bytes)
	}

	var userSubscribes []UserSubscribeInfo
	if err := tx.Table("user_subscribe").
		Select("id, user_id, upload, download, (upload + download) as used_traffic").
		Where("group_locked = ? AND status IN (0, 1)", 0). // 只查询有效且未锁定的用户订阅
		Scan(&userSubscribes).Error; err != nil {
		l.Errorw("failed to query user subscribes", logger.Field("error", err.Error()))
		return 0, err
	}

	if len(userSubscribes) == 0 {
		l.Infow("no valid and unlocked user subscribes found")
		return 0, nil
	}

	l.Infow("found user subscribes for traffic-based grouping", logger.Field("count", len(userSubscribes)))

	// 3. 根据流量范围分配节点组ID到用户订阅
	affectedCount := 0
	groupUserCount := make(map[int64]int) // node_group_id -> user_count

	for _, us := range userSubscribes {
		// 将字节转换为 GB
		usedTrafficGB := float64(us.UsedTraffic) / (1024 * 1024 * 1024)

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
		if err := tx.Table("user_subscribe").
			Where("id = ?", us.Id).
			Update("node_group_id", targetNodeGroupId).Error; err != nil {
			l.Errorw("failed to update user subscribe node_group_id",
				logger.Field("user_subscribe_id", us.Id),
				logger.Field("target_node_group_id", targetNodeGroupId),
				logger.Field("error", err.Error()))
			continue
		}

		// 只有分配了节点组的用户才记录到历史
		if targetNodeGroupId > 0 {
			// 查询用户邮箱，用于保存到历史记录
			email := l.getUserEmail(tx, us.UserId)
			userInfo := UserInfo{
				Id:    us.UserId,
				Email: email,
			}
			groupUsersMap[targetNodeGroupId] = append(groupUsersMap[targetNodeGroupId], userInfo)
			groupUserCount[targetNodeGroupId]++

			l.Debugf("assigned user subscribe %d (traffic: %.2fGB) to node group %d",
				us.Id, usedTrafficGB, targetNodeGroupId)
		} else {
			l.Debugf("user subscribe %d (traffic: %.2fGB) not assigned to any node group",
				us.Id, usedTrafficGB)
		}

		affectedCount++
	}

	l.Infof("traffic-based grouping completed: affected_subscribes=%d", affectedCount)

	// 4. 创建分组历史详情记录（只统计有用户的节点组）
	nodeGroupCount := make(map[int64]int) // node_group_id -> node_count
	for _, ng := range nodeGroups {
		nodeGroupCount[ng.Id] = 1 // 每个节点组计为1
	}

	for nodeGroupId, userCount := range groupUserCount {
		userDataJSON, err := json.Marshal(groupUsersMap[nodeGroupId])
		if err != nil {
			l.Errorw("failed to marshal user data",
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
			continue
		}

		detail := group.GroupHistoryDetail{
			HistoryId:   historyId,
			NodeGroupId: nodeGroupId,
			UserCount:   userCount,
			NodeCount:   nodeGroupCount[nodeGroupId],
			UserData:    string(userDataJSON),
		}
		if err := tx.Create(&detail).Error; err != nil {
			l.Errorw("failed to create group history detail",
				logger.Field("history_id", historyId),
				logger.Field("node_group_id", nodeGroupId),
				logger.Field("error", err.Error()))
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
