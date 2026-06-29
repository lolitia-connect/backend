package subscribe

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/server/adapter"
	"github.com/perfect-panel/server/internal/model/client"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/report"

	"github.com/perfect-panel/server/internal/model/user"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

//goland:noinspection GoNameStartsWithPackageName
type SubscribeLogic struct {
	ctx     context.Context
	svc     *svc.ServiceContext
	request RequestMeta
	logger.Logger
}

type RequestMeta struct {
	Host       string
	RequestURI string
	UserAgent  string
	ClientIP   string
}

func NewSubscribeLogic(ctx context.Context, svc *svc.ServiceContext, request RequestMeta) *SubscribeLogic {
	return &SubscribeLogic{
		ctx:     ctx,
		svc:     svc,
		request: request,
		Logger:  logger.WithContext(ctx),
	}
}

func (l *SubscribeLogic) Handler(req *types.SubscribeRequest) (resp *types.SubscribeResponse, err error) {
	// query client list
	clients, err := l.svc.Store.Client().List(l.ctx)
	if err != nil {
		l.Errorw("[SubscribeLogic] Query client list failed", logger.Field("error", err.Error()))
		return nil, err
	}

	userAgent := strings.ToLower(l.request.UserAgent)

	var targetApp, defaultApp *client.SubscribeApplication

	// If agent parameter is provided, try exact match first
	if req.Agent != "" {
		agentLower := strings.ToLower(strings.TrimSpace(req.Agent))
		for _, item := range clients {
			if strings.ToLower(item.UserAgent) == agentLower {
				targetApp = item
				l.Debugf("[SubscribeLogic] Exact agent match found: %s", item.UserAgent)
				break
			}
		}
	}

	// If no exact agent match, fall back to default UA fuzzy matching
	if targetApp == nil {
		for _, item := range clients {
			u := strings.ToLower(item.UserAgent)
			if item.IsDefault {
				defaultApp = item
			}

			if strings.Contains(userAgent, u) {
				// Special handling for Stash
				if strings.Contains(userAgent, "stash") && !strings.Contains(u, "stash") {
					continue
				}
				targetApp = item
				break
			}
		}
	} else {
		// Still find the default app in case needed
		for _, item := range clients {
			if item.IsDefault {
				defaultApp = item
				break
			}
		}
	}

	if targetApp == nil {
		l.Debugf("[SubscribeLogic] No matching client found", logger.Field("userAgent", userAgent))
		if defaultApp == nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "No matching client found for user agent: %s", userAgent)
		}
		targetApp = defaultApp
	}
	// Find user subscribe by token
	userSubscribe, err := l.getUserSubscribe(req.Token)
	if err != nil {
		l.Errorw("[SubscribeLogic] Get user subscribe failed", logger.Field("error", err.Error()), logger.Field("token", req.Token))
		return nil, err
	}

	var subscribeStatus = false
	defer func() {
		l.logSubscribeActivity(subscribeStatus, userSubscribe, req)
	}()
	// find subscribe info
	subscribeInfo, err := l.svc.Store.Subscribe().FindOne(l.ctx, userSubscribe.SubscribeId)
	if err != nil {
		l.Errorw("[SubscribeLogic] Find subscribe info failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find subscribe info failed: %v", err.Error())
	}

	// Find server list by user subscribe
	servers, err := l.getServers(userSubscribe, req.Type)
	if err != nil {
		return nil, err
	}
	a := adapter.NewAdapter(
		targetApp.SubscribeTemplate,
		adapter.WithServers(servers),
		adapter.WithSiteName(l.svc.Config.Site.SiteName),
		adapter.WithSubscribeName(subscribeInfo.Name),
		adapter.WithOutputFormat(targetApp.OutputFormat),
		adapter.WithUserInfo(adapter.User{
			Password:         userSubscribe.UUID,
			ExpiredAt:        userSubscribe.ExpireTime,
			Download:         userSubscribe.Download,
			Upload:           userSubscribe.Upload,
			Traffic:          userSubscribe.Traffic,
			TrafficUnlimited: userSubscribe.TrafficUnlimited,
			SubscribeURL:     l.getSubscribeV2URL(),
		}),
		adapter.WithParams(req.Params),
	)

	logger.Debugf("[SubscribeLogic] Building client config for user %d with URI %s", userSubscribe.UserId, l.getSubscribeV2URL())

	// Get client config
	adapterClient, err := a.Client()
	if err != nil {
		l.Errorw("[SubscribeLogic] Client error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(500), "Client error: %v", err.Error())
	}
	bytes, err := adapterClient.Build()
	if err != nil {
		l.Errorw("[SubscribeLogic] Build client config failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(500), "Build client config failed: %v", err.Error())
	}

	var formats = []string{"json", "yaml", "conf"}

	headers := make(map[string]string)
	for _, format := range formats {
		if format == strings.ToLower(targetApp.OutputFormat) {
			headers["Content-Disposition"] = fmt.Sprintf("attachment;filename*=UTF-8''%s.%s", url.QueryEscape(l.svc.Config.Site.SiteName), format)
			headers["Content-Type"] = "application/octet-stream; charset=UTF-8"
		}
	}

	resp = &types.SubscribeResponse{
		Config: bytes,
		Header: fmt.Sprintf(
			"upload=%d;download=%d;total=%d;expire=%d",
			userSubscribe.Upload, userSubscribe.Download,
			func() int64 {
				if userSubscribe.TrafficUnlimited {
					return 0
				}
				return userSubscribe.Traffic
			}(),
			userSubscribe.ExpireTime.Unix(),
		),
		Headers: headers,
	}
	subscribeStatus = true
	return
}

func (l *SubscribeLogic) getSubscribeV2URL() string {

	uri := l.request.RequestURI
	// is gateway mode, add /sub prefix
	if report.IsGatewayMode() {
		uri = "/sub" + uri
	}
	// use custom domain if configured
	if l.svc.Config.Subscribe.SubscribeDomain != "" {
		domains := strings.Split(l.svc.Config.Subscribe.SubscribeDomain, "\n")
		return fmt.Sprintf("https://%s%s", domains[0], uri)
	}
	// use current request host
	return fmt.Sprintf("https://%s%s", l.request.Host, uri)
}

// getUserSubscribe 是本次修改的核心部分
func (l *SubscribeLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.svc.Store.User().FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	if userSub == nil {
		l.Infow("[Generate Subscribe]subscribe token not found", logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe token not found")
	}
	userInfo, err := l.svc.Store.User().FindOne(l.ctx, userSub.UserId)
	if err != nil {
		l.Infow("[Generate Subscribe] failed to get user info", logger.Field("error", err.Error()), logger.Field("userId", userSub.UserId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to get user info: %v", err.Error())
	}
	if !*userInfo.Enable {
		l.Infow("[Generate Subscribe] user account is disabled", logger.Field("userId", userSub.UserId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserDisabled), "User account is disabled")
	}

	//  Ignore expiration check
	//if userSub.Status > 1 {
	// l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	// return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
	//}

	return userSub, nil
}

func (l *SubscribeLogic) logSubscribeActivity(subscribeStatus bool, userSub *user.Subscribe, req *types.SubscribeRequest) {
	if !subscribeStatus {
		return
	}

	subscribeLog := log.Subscribe{
		Token:           req.Token,
		UserAgent:       req.UA,
		ClientIP:        l.request.ClientIP,
		UserSubscribeId: userSub.Id,
	}

	content, _ := subscribeLog.Marshal()

	err := l.svc.Store.Log().Insert(l.ctx, &log.SystemLog{
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: userSub.UserId, // log user id
		Date:     time.Now().Format(time.DateOnly),
		Content:  string(content),
	})
	if err != nil {
		l.Errorw("[Generate Subscribe]insert subscribe log error: %v", logger.Field("error", err.Error()))
	}
}

func (l *SubscribeLogic) getServers(userSub *user.Subscribe, protocolType string) ([]*node.Node, error) {
	if l.isSubscriptionExpired(userSub) {
		// 尝试获取过期节点组的节点
		expiredNodes, err := l.getExpiredGroupNodes(userSub)
		if err != nil {
			l.Errorw("[Generate Subscribe]get expired group nodes error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
		}
		// 如果有符合条件的过期节点组节点，返回它们
		if len(expiredNodes) > 0 {
			// 按协议类型过滤节点
			if protocolType != "" {
				expiredNodes = filterNodesByProtocol(expiredNodes, protocolType)
				l.Debugf("[Generate Subscribe]filtered expired nodes by protocol %s, remaining: %d", protocolType, len(expiredNodes))
			}
			l.Debugf("[Generate Subscribe]user %d can use expired node group, nodes count: %d", userSub.UserId, len(expiredNodes))
			return expiredNodes, nil
		}
		// 没有配置过期节点组或不符合条件，返回 404
		l.Debugf("[Generate Subscribe]user %d cannot use expired node group, return 404", userSub.UserId)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	if l.isTrafficExhausted(userSub) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TrafficExhausted), "traffic exhausted")
	}

	subDetails, err := l.svc.Store.Subscribe().FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Errorw("[Generate Subscribe]find subscribe details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe details error: %v", err.Error())
	}

	// 判断是否使用分组模式
	isGroupMode := l.isGroupEnabled()

	if isGroupMode {
		// === 分组模式：使用 node_group_ids 获取节点 ===
		// 收集所有分组 ID：主节点组 + 备用节点组，自动去重
		// 优先级：user_subscribe.node_group_id > subscribe.node_group_id + subscribe.node_group_ids
		allGroupIds := collectGroupIds(userSub.NodeGroupId, subDetails.NodeGroupId, subDetails.NodeGroupIds)
		source := "subscribe.node_group_id + node_group_ids"
		if userSub.NodeGroupId != 0 {
			source = "user_subscribe.node_group_id"
		}

		l.Debugf("[Generate Subscribe]group mode, using %s: allGroupIds=%v", source, allGroupIds)

		// 过滤掉不可访问的节点组，并找到主节点组（用于标签显示）
		var currentNodeGroup *group.NodeGroup
		var accessibleGroupIds []int64
		for _, gid := range allGroupIds {
			ng := l.getAccessibleNodeGroup(gid, group.NodeGroupAccessSubscribe)
			if ng != nil {
				accessibleGroupIds = append(accessibleGroupIds, gid)
				if currentNodeGroup == nil {
					currentNodeGroup = ng
				}
			} else {
				l.Debugf("[Generate Subscribe]node group %d is not accessible for subscribe output, skipping", gid)
			}
		}
		allGroupIds = accessibleGroupIds

		if len(allGroupIds) == 0 {
			l.Debugf("[Generate Subscribe]no accessible node groups found")
		}

		// 根据所有 node_group_ids 获取节点
		enable := true
		isHidden := false

		// 1. 获取分组节点（主节点组 + 备用节点组的所有节点，自动去重）
		var groupNodes []*node.Node
		if len(allGroupIds) > 0 {
			params := &node.FilterNodeParams{
				Page:         1,
				Size:         1000,
				NodeGroupIds: allGroupIds,
				Enabled:      &enable,
				IsHidden:     &isHidden,
				Preload:      true,
			}
			_, groupNodes, err = l.svc.Store.Node().FilterNodeList(l.ctx, params)

			if err != nil {
				l.Errorw("[Generate Subscribe]filter nodes by group error", logger.Field("error", err.Error()))
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "filter nodes by group error: %v", err.Error())
			}
			l.Debugf("[Generate Subscribe]found %d nodes for node_group_ids=%v", len(groupNodes), allGroupIds)
		}

		// 2. 获取公共节点（NodeGroupIds 为空的节点）
		_, allNodes, err := l.svc.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:     1,
			Size:     1000,
			Enabled:  &enable,
			IsHidden: &isHidden,
			Preload:  true,
		})

		if err != nil {
			l.Errorw("[Generate Subscribe]filter all nodes error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "filter all nodes error: %v", err.Error())
		}

		// 过滤出公共节点
		var publicNodes []*node.Node
		for _, n := range allNodes {
			if len(n.NodeGroupIds) == 0 {
				publicNodes = append(publicNodes, n)
			}
		}
		l.Debugf("[Generate Subscribe]found %d public nodes (node_group_ids is empty)", len(publicNodes))

		// 3. 合并分组节点和公共节点（按 Id 去重，分组节点优先）
		result := mergeGroupAndPublicNodes(groupNodes, publicNodes)

		l.Debugf("[Generate Subscribe]total nodes (group + public): %d (group: %d, public: %d)", len(result), len(groupNodes), len(publicNodes))

		// 查询节点组信息，获取节点组名称（仅当用户有分组时）
		if currentNodeGroup != nil && currentNodeGroup.Name != "" {
			for _, n := range result {
				// 只为分组节点设置 tag，公共节点不设置
				if n.Tags == "" && len(n.NodeGroupIds) > 0 {
					n.Tags = currentNodeGroup.Name
					l.Debugf("[Generate Subscribe]set node_group name as tag for node %d: %s", n.Id, currentNodeGroup.Name)
				}
			}
		}

		// 按协议类型过滤节点
		if protocolType != "" {
			result = filterNodesByProtocol(result, protocolType)
			l.Debugf("[Generate Subscribe]filtered by protocol %s, remaining nodes: %d", protocolType, len(result))
		}

		return result, nil
	}

	// === 标签模式：使用 node_ids 和 tags 获取节点 ===
	nodeIds := tool.StringToInt64Slice(subDetails.Nodes)
	tags := tool.RemoveStringElement(strings.Split(subDetails.NodeTags, ","), "")

	l.Debugf("[Generate Subscribe]tag mode, nodes: %v, NodeTags: %v", len(nodeIds), len(tags))
	if len(nodeIds) == 0 && len(tags) == 0 {
		logger.Infow("[Generate Subscribe]no subscribe nodes configured")
		return []*node.Node{}, nil
	}

	enable := true
	var nodes []*node.Node
	_, nodes, err = l.svc.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    1,
		Size:    1000,
		NodeId:  nodeIds,
		Tag:     tool.RemoveDuplicateElements(tags...),
		Preload: true,
		Enabled: &enable, // Only get enabled nodes
	})

	if err != nil {
		l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
	}

	l.Debugf("[Generate Subscribe]found %d nodes in tag mode", len(nodes))

	// 按协议类型过滤节点
	if protocolType != "" {
		nodes = filterNodesByProtocol(nodes, protocolType)
		l.Debugf("[Generate Subscribe]filtered by protocol %s, remaining nodes: %d", protocolType, len(nodes))
	}

	return nodes, nil
}

func (l *SubscribeLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < time.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

// isGroupEnabled 判断分组功能是否启用
func (l *SubscribeLogic) isGroupEnabled() bool {
	var value string
	err := l.svc.Store.DB().Table("system").
		Where("`category` = ? AND `key` = ?", "group", "enabled").
		Select("value").
		Scan(&value).Error
	if err != nil {
		l.Debugf("[SubscribeLogic]check group enabled failed: %v", err)
		return false
	}
	return value == "true" || value == "1"
}

// getExpiredGroupNodes 获取过期节点组的节点
func (l *SubscribeLogic) getExpiredGroupNodes(userSub *user.Subscribe) ([]*node.Node, error) {
	// 1. 查询过期节点组
	var expiredGroup group.NodeGroup
	err := l.svc.Store.DB().Where("is_expired_group = ?", true).Find(&expiredGroup).Error
	if err != nil {
		l.Debugw("[SubscribeLogic]no expired node group configured", logger.Field("error", err.Error()))
		return nil, err
	}
	if expiredGroup.Id == 0 {
		l.Debugw("[SubscribeLogic]no expired node group configured")
		return nil, nil
	}
	if !group.IsNodeGroupTypeAccessible(expiredGroup.Type, group.NodeGroupAccessSubscribe) {
		l.Debugf("[SubscribeLogic]expired node group %d is not accessible for subscribe output", expiredGroup.Id)
		return nil, nil
	}

	// 2. 检查用户是否在过期天数限制内
	expiredDays := int(time.Since(userSub.ExpireTime).Hours() / 24)
	if expiredDays > expiredGroup.ExpiredDaysLimit {
		l.Debugf("[SubscribeLogic]user %d subscription expired %d days, exceeds limit %d days", userSub.UserId, expiredDays, expiredGroup.ExpiredDaysLimit)
		return nil, nil
	}

	// 3. 检查用户已使用流量是否超过限制(仅使用过期期间的流量)
	if expiredGroup.MaxTrafficGBExpired != nil && *expiredGroup.MaxTrafficGBExpired > 0 {
		usedTrafficGB := (userSub.ExpiredDownload + userSub.ExpiredUpload) / (1024 * 1024 * 1024)
		if usedTrafficGB >= *expiredGroup.MaxTrafficGBExpired {
			l.Debugf("[SubscribeLogic]user %d expired traffic %d GB, exceeds expired group limit %d GB", userSub.UserId, usedTrafficGB, *expiredGroup.MaxTrafficGBExpired)
			return nil, nil
		}
	}

	// 4. 查询过期节点组的节点
	enable := true
	isHidden := false
	_, nodes, err := l.svc.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:         1,
		Size:         1000,
		NodeGroupIds: []int64{expiredGroup.Id},
		Enabled:      &enable,
		IsHidden:     &isHidden,
		Preload:      true,
	})
	if err != nil {
		l.Errorw("[SubscribeLogic]failed to query expired group nodes", logger.Field("error", err.Error()))
		return nil, err
	}

	if len(nodes) == 0 {
		l.Debug("[SubscribeLogic]no nodes found in expired group")
		return nil, nil
	}

	l.Infof("[SubscribeLogic]returned %d nodes from expired group for user %d (expired %d days)", len(nodes), userSub.UserId, expiredDays)
	return nodes, nil
}

// isTrafficExhausted reports whether the subscription has used up its traffic
// quota. Traffic == 0 means unlimited. Mirrors the condition used by
// FindTrafficExceededSubscribes (upload + download >= traffic AND traffic > 0).
func (l *SubscribeLogic) isTrafficExhausted(userSub *user.Subscribe) bool {
	return userSub.Traffic > 0 && userSub.Download+userSub.Upload >= userSub.Traffic
}

func (l *SubscribeLogic) getAccessibleNodeGroup(nodeGroupId int64, accessType string) *group.NodeGroup {
	if nodeGroupId == 0 {
		return nil
	}

	var nodeGroup group.NodeGroup
	if err := l.svc.Store.DB().Select("id, name, group_type").Where("id = ?", nodeGroupId).First(&nodeGroup).Error; err != nil {
		l.Infow("[Generate Subscribe]node group not found", logger.Field("nodeGroupId", nodeGroupId), logger.Field("error", err.Error()))
		return nil
	}

	if !group.IsNodeGroupTypeAccessible(nodeGroup.Type, accessType) {
		return nil
	}

	nodeGroup.Type = group.MustNodeGroupType(nodeGroup.Type)
	return &nodeGroup
}

// filterNodesByProtocol 按协议类型过滤节点
func filterNodesByProtocol(nodes []*node.Node, protocolType string) []*node.Node {
	protocolLower := strings.ToLower(protocolType)
	var filtered []*node.Node
	for _, n := range nodes {
		if strings.ToLower(n.Protocol) == protocolLower {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// collectGroupIds 收集所有分组 ID：主节点组 + 备用节点组，自动去重
// 优先级：userSubNodeGroupId > subNodeGroupId（作为主节点组）
// 始终合并 subNodeGroupIds 中的备用节点组
func collectGroupIds(userSubNodeGroupId int64, subNodeGroupId int64, subNodeGroupIds []int64) []int64 {
	var result []int64
	seen := make(map[int64]bool)

	// 确定主节点组
	primaryId := subNodeGroupId
	if userSubNodeGroupId != 0 {
		primaryId = userSubNodeGroupId
	}
	if primaryId != 0 {
		result = append(result, primaryId)
		seen[primaryId] = true
	}

	// 合并所有备用节点组（去重）
	for _, gid := range subNodeGroupIds {
		if gid > 0 && !seen[gid] {
			result = append(result, gid)
			seen[gid] = true
		}
	}

	return result
}

// mergeGroupAndPublicNodes 合并分组节点和公共节点，按 Id 去重，保持 sort 顺序
// 分组节点优先：如果同一 Id 同时出现在分组和公共节点中，保留分组版本
func mergeGroupAndPublicNodes(groupNodes, publicNodes []*node.Node) []*node.Node {
	nodesMap := make(map[int64]*node.Node)
	var order []int64

	for _, n := range groupNodes {
		if _, exists := nodesMap[n.Id]; !exists {
			order = append(order, n.Id)
		}
		nodesMap[n.Id] = n
	}
	for _, n := range publicNodes {
		if _, exists := nodesMap[n.Id]; !exists {
			order = append(order, n.Id)
			nodesMap[n.Id] = n
		}
	}

	result := make([]*node.Node, 0, len(order))
	for _, id := range order {
		result = append(result, nodesMap[id])
	}
	return result
}
