package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetServerUserListLogic struct {
	logger.Logger
	ctx    *gin.Context
	svcCtx *svc.ServiceContext
}

// NewGetServerUserListLogic Get user list
func NewGetServerUserListLogic(ctx *gin.Context, svcCtx *svc.ServiceContext) *GetServerUserListLogic {
	return &GetServerUserListLogic{
		Logger: logger.WithContext(ctx.Request.Context()),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerUserListLogic) GetServerUserList(req *types.GetServerUserListRequest) (resp *types.GetServerUserListResponse, err error) {
	cacheKey := fmt.Sprintf("%s%d", node.ServerUserListCacheKey, req.ServerId)
	cache, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if cache != "" {
		etag := tool.GenerateETag([]byte(cache))
		resp = &types.GetServerUserListResponse{}
		//  Check If-None-Match header
		if match := l.ctx.GetHeader("If-None-Match"); match == etag {
			return nil, xerr.StatusNotModified
		}
		l.ctx.Header("ETag", etag)
		err = json.Unmarshal([]byte(cache), resp)
		if err != nil {
			l.Errorw("[ServerUserListCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, err
		}
		return resp, nil
	}
	server, err := l.svcCtx.NodeModel.FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		return nil, err
	}

	// 查询该服务器上该协议的所有节点（包括属于节点组的节点）
	_, nodes, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{server.Id},
		Protocol: req.Protocol,
	})
	if err != nil {
		l.Errorw("FilterNodeList error", logger.Field("error", err.Error()))
		return nil, err
	}

	if len(nodes) == 0 {
		return &types.GetServerUserListResponse{
			Users: []types.ServerUser{
				{
					Id:   1,
					UUID: uuidx.NewUUID().String(),
				},
			},
		}, nil
	}

	// 收集所有唯一的节点组 ID
	nodeGroupMap := make(map[int64]bool) // nodeGroupId -> true
	var nodeIds []int64
	var nodeTags []string

	for _, n := range nodes {
		nodeIds = append(nodeIds, n.Id)
		if n.Tags != "" {
			nodeTags = append(nodeTags, strings.Split(n.Tags, ",")...)
		}
		// 收集节点组 ID
		if len(n.NodeGroupIds) > 0 {
			for _, gid := range n.NodeGroupIds {
				if gid > 0 {
					nodeGroupMap[gid] = true
				}
			}
		}
	}

	// 获取所有节点组 ID
	nodeGroupIds := make([]int64, 0, len(nodeGroupMap))
	for gid := range nodeGroupMap {
		nodeGroupIds = append(nodeGroupIds, gid)
	}

	// 查询订阅：
	// 1. 如果有节点组，查询匹配这些节点组的订阅
	// 2. 如果没有节点组，查询使用节点 ID 或 tags 的订阅
	var subs []*subscribe.Subscribe
	if len(nodeGroupIds) > 0 {
		// 节点组模式：查询 node_group_id 或 node_group_ids 匹配的订阅
		_, subs, err = l.svcCtx.SubscribeModel.FilterListByNodeGroups(l.ctx, &subscribe.FilterByNodeGroupsParams{
			Page:         1,
			Size:         9999,
			NodeGroupIds: nodeGroupIds,
		})
		if err != nil {
			l.Errorw("FilterListByNodeGroups error", logger.Field("error", err.Error()))
			return nil, err
		}
	} else {
		// 传统模式：查询匹配节点 ID 或 tags 的订阅
		nodeTags = tool.RemoveDuplicateElements(nodeTags...)
		_, subs, err = l.svcCtx.SubscribeModel.FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Node: nodeIds,
			Tags: nodeTags,
		})
		if err != nil {
			l.Errorw("FilterList error", logger.Field("error", err.Error()))
			return nil, err
		}
	}
	
	if len(subs) == 0 {
		return &types.GetServerUserListResponse{
			Users: []types.ServerUser{
				{
					Id:   1,
					UUID: uuidx.NewUUID().String(),
				},
			},
		}, nil
	}
	users := make([]types.ServerUser, 0)
	for _, sub := range subs {
		data, err := l.svcCtx.UserModel.FindUsersSubscribeBySubscribeId(l.ctx, sub.Id)
		if err != nil {
			return nil, err
		}
		for _, datum := range data {
			if !l.shouldIncludeServerUser(datum, nodeGroupIds) {
				continue
			}

			// 计算该用户的实际限速值（考虑按量限速规则）
			effectiveSpeedLimit := l.calculateEffectiveSpeedLimit(sub, datum)

			users = append(users, types.ServerUser{
				Id:          datum.Id,
				UUID:        datum.UUID,
				SpeedLimit:  effectiveSpeedLimit,
				DeviceLimit: sub.DeviceLimit,
			})
		}
	}

	// 处理过期订阅用户：如果当前节点属于过期节点组，添加符合条件的过期用户
	if len(nodeGroupIds) > 0 {
		expiredUsers, expiredSpeedLimit := l.getExpiredUsers(nodeGroupIds)
		for i := range expiredUsers {
			if expiredSpeedLimit > 0 {
				expiredUsers[i].SpeedLimit = expiredSpeedLimit
			}
		}
		users = append(users, expiredUsers...)
	}

	if len(users) == 0 {
		users = append(users, types.ServerUser{
			Id:   1,
			UUID: uuidx.NewUUID().String(),
		})
	}
	resp = &types.GetServerUserListResponse{
		Users: users,
	}
	val, _ := json.Marshal(resp)
	etag := tool.GenerateETag(val)
	l.ctx.Header("ETag", etag)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), -1).Err()
	if err != nil {
		l.Errorw("[ServerUserListCacheKey] redis set error", logger.Field("error", err.Error()))
	}
	//  Check If-None-Match header
	if match := l.ctx.GetHeader("If-None-Match"); match == etag {
		return nil, xerr.StatusNotModified
	}
	return resp, nil
}

func (l *GetServerUserListLogic) shouldIncludeServerUser(userSub *user.Subscribe, serverNodeGroupIds []int64) bool {
	if userSub == nil {
		return false
	}

	if userSub.ExpireTime.Unix() == 0 || userSub.ExpireTime.After(time.Now()) {
		return true
	}

	return l.canUseExpiredNodeGroup(userSub, serverNodeGroupIds)
}

func (l *GetServerUserListLogic) getExpiredUsers(serverNodeGroupIds []int64) ([]types.ServerUser, int64) {
	var expiredGroup group.NodeGroup
	if err := l.svcCtx.DB.Where("is_expired_group = ?", true).First(&expiredGroup).Error; err != nil {
		return nil, 0
	}

	if !tool.Contains(serverNodeGroupIds, expiredGroup.Id) {
		return nil, 0
	}

	var expiredSubs []*user.Subscribe
	if err := l.svcCtx.DB.Where("status = ?", 3).Find(&expiredSubs).Error; err != nil {
		l.Errorw("query expired subscriptions failed", logger.Field("error", err.Error()))
		return nil, 0
	}

	users := make([]types.ServerUser, 0)
	seen := make(map[int64]bool)
	for _, userSub := range expiredSubs {
		if !l.checkExpiredUserEligibility(userSub, &expiredGroup) {
			continue
		}
		if seen[userSub.Id] {
			continue
		}
		seen[userSub.Id] = true
		users = append(users, types.ServerUser{
			Id:   userSub.Id,
			UUID: userSub.UUID,
		})
	}

	return users, int64(expiredGroup.SpeedLimit)
}

func (l *GetServerUserListLogic) checkExpiredUserEligibility(userSub *user.Subscribe, expiredGroup *group.NodeGroup) bool {
	expiredDays := int(time.Since(userSub.ExpireTime).Hours() / 24)
	if expiredDays > expiredGroup.ExpiredDaysLimit {
		return false
	}

	if expiredGroup.MaxTrafficGBExpired != nil && *expiredGroup.MaxTrafficGBExpired > 0 {
		usedTrafficGB := (userSub.ExpiredDownload + userSub.ExpiredUpload) / (1024 * 1024 * 1024)
		if usedTrafficGB >= *expiredGroup.MaxTrafficGBExpired {
			return false
		}
	}

	return true
}

func (l *GetServerUserListLogic) canUseExpiredNodeGroup(userSub *user.Subscribe, serverNodeGroupIds []int64) bool {
	var expiredGroup group.NodeGroup
	if err := l.svcCtx.DB.Where("is_expired_group = ?", true).First(&expiredGroup).Error; err != nil {
		return false
	}

	if !tool.Contains(serverNodeGroupIds, expiredGroup.Id) {
		return false
	}

	expiredDays := int(time.Since(userSub.ExpireTime).Hours() / 24)
	if expiredDays > expiredGroup.ExpiredDaysLimit {
		return false
	}

	if expiredGroup.MaxTrafficGBExpired != nil && *expiredGroup.MaxTrafficGBExpired > 0 {
		usedTrafficGB := (userSub.ExpiredDownload + userSub.ExpiredUpload) / (1024 * 1024 * 1024)
		if usedTrafficGB >= *expiredGroup.MaxTrafficGBExpired {
			return false
		}
	}

	return true
}

// calculateEffectiveSpeedLimit 计算用户的实际限速值（考虑按量限速规则）
func (l *GetServerUserListLogic) calculateEffectiveSpeedLimit(sub *subscribe.Subscribe, userSub *user.Subscribe) int64 {
	baseSpeedLimit := sub.SpeedLimit

	// 解析 traffic_limit 规则
	if sub.TrafficLimit == "" {
		return baseSpeedLimit
	}

	var trafficLimitRules []types.TrafficLimit
	if err := json.Unmarshal([]byte(sub.TrafficLimit), &trafficLimitRules); err != nil {
		l.Errorw("[calculateEffectiveSpeedLimit] Failed to unmarshal traffic_limit",
			logger.Field("error", err.Error()),
			logger.Field("traffic_limit", sub.TrafficLimit))
		return baseSpeedLimit
	}

	if len(trafficLimitRules) == 0 {
		return baseSpeedLimit
	}

	// 查询用户指定时段的流量使用情况
	now := time.Now()
	for _, rule := range trafficLimitRules {
		var startTime, endTime time.Time

		if rule.StatType == "hour" {
			// 按小时统计：根据 StatValue 计算时间范围（往前推 N 小时）
			if rule.StatValue <= 0 {
				continue
			}
			// 从当前时间往前推 StatValue 小时
			startTime = now.Add(-time.Duration(rule.StatValue) * time.Hour)
			endTime = now
		} else if rule.StatType == "day" {
			// 按天统计：根据 StatValue 计算时间范围（往前推 N 天）
			if rule.StatValue <= 0 {
				continue
			}
			// 从当前时间往前推 StatValue 天
			startTime = now.AddDate(0, 0, -int(rule.StatValue))
			endTime = now
		} else {
			continue
		}

		// 查询该时段的流量使用
		var usedTraffic struct {
			Upload   int64
			Download int64
		}
		err := l.svcCtx.DB.WithContext(l.ctx.Request.Context()).
			Table("traffic_log").
			Select("COALESCE(SUM(upload), 0) as upload, COALESCE(SUM(download), 0) as download").
			Where("user_id = ? AND subscribe_id = ? AND timestamp >= ? AND timestamp < ?",
				userSub.UserId, userSub.Id, startTime, endTime).
			Scan(&usedTraffic).Error

		if err != nil {
			l.Errorw("[calculateEffectiveSpeedLimit] Failed to query traffic usage",
				logger.Field("error", err.Error()),
				logger.Field("user_id", userSub.UserId),
				logger.Field("subscribe_id", userSub.Id))
			continue
		}

		// 计算已使用流量（GB）
		usedGB := float64(usedTraffic.Upload+usedTraffic.Download) / (1024 * 1024 * 1024)

		// 如果已使用流量达到或超过阈值，应用限速
		if usedGB >= float64(rule.TrafficUsage) {
			// 如果规则限速大于0，应用该限速
			if rule.SpeedLimit > 0 {
				// 如果基础限速为0（无限速）或规则限速更严格，使用规则限速
				if baseSpeedLimit == 0 || rule.SpeedLimit < baseSpeedLimit {
					return rule.SpeedLimit
				}
			}
		}
	}

	return baseSpeedLimit
}
