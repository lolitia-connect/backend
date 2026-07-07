package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"go.uber.org/zap"
)

type GetServerUserListLogic struct {
	Logger   *zap.SugaredLogger
	ctx      context.Context
	svcCtx   *svc.ServiceContext
	request  RequestMeta
	response ResponseMeta
}

// NewGetServerUserListLogic Get user list
func NewGetServerUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext, request RequestMeta) *GetServerUserListLogic {
	return &GetServerUserListLogic{
		Logger:   zap.S(),
		ctx:      ctx,
		svcCtx:   svcCtx,
		request:  request,
		response: NewResponseMeta(),
	}
}

func (l *GetServerUserListLogic) ResponseMeta() ResponseMeta {
	return l.response
}

func placeholderServerUser(serverID int64, protocol, secret string) types.ServerUser {
	name := fmt.Sprintf("ppanel:server-user-placeholder:%d:%s:%s", serverID, strings.TrimSpace(protocol), secret)
	return types.ServerUser{
		Id:   1,
		UUID: uuidx.NewDeterministicUUID(name).String(),
	}
}

func mergeSubscribeLists(lists ...[]*subscribe.Subscribe) []*subscribe.Subscribe {
	seen := make(map[int64]struct{})
	result := make([]*subscribe.Subscribe, 0)
	for _, list := range lists {
		for _, item := range list {
			if item == nil {
				continue
			}
			if _, ok := seen[item.Id]; ok {
				continue
			}
			seen[item.Id] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func (l *GetServerUserListLogic) queryMatchedSubscribes(nodeIds []int64, nodeTags []string) ([]*subscribe.Subscribe, error) {
	var lists [][]*subscribe.Subscribe
	if len(nodeIds) > 0 {
		_, subs, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Node: nodeIds,
		})
		if err != nil {
			return nil, err
		}
		lists = append(lists, subs)
	}

	nodeTags = tool.RemoveDuplicateElements(nodeTags...)
	if len(nodeTags) > 0 {
		_, subs, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Tags: nodeTags,
		})
		if err != nil {
			return nil, err
		}
		lists = append(lists, subs)
	}

	return mergeSubscribeLists(lists...), nil
}

func (l *GetServerUserListLogic) GetServerUserList(req *types.GetServerUserListRequest) (resp *types.GetServerUserListResponse, err error) {
	cacheKey := fmt.Sprintf("%s%d:%s:%s", node.ServerUserListCacheKey, req.ServerId, req.Protocol, req.ProtocolId)
	cache, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if cache != "" {
		etag := tool.GenerateETag([]byte(cache))
		resp = &types.GetServerUserListResponse{}
		//  Check If-None-Match header
		if match := l.request.IfNoneMatch; match == etag {
			return nil, xerr.StatusNotModified
		}
		l.response.SetHeader("ETag", etag)
		err = json.Unmarshal([]byte(cache), resp)
		if err != nil {
			l.Logger.Errorw("[ServerUserListCacheKey] json unmarshal error", zap.Any("error", err.Error()))
			return nil, err
		}
		return resp, nil
	}
	server, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		return nil, err
	}

	_, nodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:       1,
		Size:       1000,
		ServerId:   []int64{server.Id},
		Protocol:   req.Protocol,
		ProtocolId: req.ProtocolId,
	})
	if err != nil {
		l.Logger.Errorw("FilterNodeList error", zap.Any("error", err.Error()))
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

	// 查询订阅：
	// 1. 如果有节点组，查询匹配这些节点组的订阅
	// 2. 如果没有节点组，查询使用节点 ID 或 tags 的订阅
	var subs []*subscribe.Subscribe
	// 将 nodeGroupMap 转换为 slice
	var nodeGroupIds []int64
	for gid := range nodeGroupMap {
		nodeGroupIds = append(nodeGroupIds, gid)
	}
	if len(nodeGroupIds) > 0 {
		// 节点组模式：查询 node_group_id 或 node_group_ids 匹配的订阅
		_, subs, err = l.svcCtx.Store.Subscribe().FilterListByNodeGroups(l.ctx, &subscribe.FilterByNodeGroupsParams{
			Page:         1,
			Size:         9999,
			NodeGroupIds: nodeGroupIds,
		})
		if err != nil {
			l.Logger.Errorw("FilterListByNodeGroups error", zap.Any("error", err.Error()))
			return nil, err
		}
	} else {
		// 传统模式：查询匹配节点 ID 或 tags 的订阅
		nodeTags = tool.RemoveDuplicateElements(nodeTags...)
		_, subs, err = l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Node: nodeIds,
			Tags: nodeTags,
		})
		if err != nil {
			l.Logger.Errorw("FilterList error", zap.Any("error", err.Error()))
			return nil, err
		}
	}

	if len(subs) == 0 {
		return &types.GetServerUserListResponse{
			Users: []types.ServerUser{placeholderServerUser(req.ServerId, req.Protocol, l.svcCtx.Config.Node.NodeSecret)},
		}, nil
	}
	users := make([]types.ServerUser, 0)
	for _, sub := range subs {
		if err := l.svcCtx.Store.User().ActivatePendingSubscribesBySubscribeId(l.ctx, sub.Id); err != nil {
			return nil, err
		}
		data, err := l.svcCtx.Store.User().FindUsersSubscribeBySubscribeId(l.ctx, sub.Id)
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
		users = append(users, placeholderServerUser(req.ServerId, req.Protocol, l.svcCtx.Config.Node.NodeSecret))
	}
	resp = &types.GetServerUserListResponse{
		Users: users,
	}
	val, _ := json.Marshal(resp)
	etag := tool.GenerateETag(val)
	l.response.SetHeader("ETag", etag)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), node.ServerCacheTTL).Err()
	if err != nil {
		l.Logger.Errorw("[ServerUserListCacheKey] redis set error", zap.Any("error", err.Error()))
	}
	//  Check If-None-Match header
	if match := l.request.IfNoneMatch; match == etag {
		return nil, xerr.StatusNotModified
	}
	return resp, nil
}

func (l *GetServerUserListLogic) shouldIncludeServerUser(userSub *user.Subscribe, serverNodeGroupIds []int64) bool {
	if userSub == nil {
		return false
	}

	if tool.Unix(userSub.ExpireTime) == 0 || userSub.ExpireTime.After(time.Now()) {
		return true
	}

	return l.canUseExpiredNodeGroup(userSub, serverNodeGroupIds)
}

func (l *GetServerUserListLogic) getExpiredUsers(serverNodeGroupIds []int64) ([]types.ServerUser, int64) {
	expiredGroup, err := l.svcCtx.Store.Group().FindExpiredNodeGroup(l.ctx)
	if err != nil {
		return nil, 0
	}

	if !tool.Contains(serverNodeGroupIds, expiredGroup.Id) {
		return nil, 0
	}

	expiredSubs, err := l.svcCtx.Store.User().FindUserSubscribesByStatus(l.ctx, 3)
	if err != nil {
		l.Logger.Errorw("query expired subscriptions failed", zap.Any("error", err.Error()))
		return nil, 0
	}

	users := make([]types.ServerUser, 0)
	seen := make(map[int64]bool)
	for _, userSub := range expiredSubs {
		if !l.checkExpiredUserEligibility(userSub, expiredGroup) {
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
	expiredGroup, err := l.svcCtx.Store.Group().FindExpiredNodeGroup(l.ctx)
	if err != nil {
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
		l.Logger.Errorw("[calculateEffectiveSpeedLimit] Failed to unmarshal traffic_limit",
			zap.Any("error", err.Error()),
			zap.Any("traffic_limit", sub.TrafficLimit))
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

		usedTraffic, err := l.svcCtx.Store.TrafficLog().SumByUserSubscribeAndTimeRange(l.ctx, userSub.UserId, userSub.Id, startTime, endTime)
		if err != nil {
			l.Logger.Errorw("[calculateEffectiveSpeedLimit] Failed to query traffic usage",
				zap.Any("error", err.Error()),
				zap.Any("user_id", userSub.UserId),
				zap.Any("subscribe_id", userSub.Id))
			continue
		}
		upload, download := int64(0), int64(0)
		if usedTraffic != nil {
			upload = usedTraffic.Upload
			download = usedTraffic.Download
		}

		// 计算已使用流量（GB）
		usedGB := float64(upload+download) / (1024 * 1024 * 1024)

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
