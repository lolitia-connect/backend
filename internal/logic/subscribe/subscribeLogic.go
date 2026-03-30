package subscribe

import (
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

	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

//goland:noinspection GoNameStartsWithPackageName
type SubscribeLogic struct {
	ctx *gin.Context
	svc *svc.ServiceContext
	logger.Logger
}

func NewSubscribeLogic(ctx *gin.Context, svc *svc.ServiceContext) *SubscribeLogic {
	return &SubscribeLogic{
		ctx:    ctx,
		svc:    svc,
		Logger: logger.WithContext(ctx.Request.Context()),
	}
}

func (l *SubscribeLogic) Handler(req *types.SubscribeRequest) (resp *types.SubscribeResponse, err error) {
	// query client list
	clients, err := l.svc.ClientModel.List(l.ctx.Request.Context())
	if err != nil {
		l.Errorw("[SubscribeLogic] Query client list failed", logger.Field("error", err.Error()))
		return nil, err
	}

	userAgent := strings.ToLower(l.ctx.Request.UserAgent())

	var targetApp, defaultApp *client.SubscribeApplication

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
	subscribeInfo, err := l.svc.SubscribeModel.FindOne(l.ctx.Request.Context(), userSubscribe.SubscribeId)
	if err != nil {
		l.Errorw("[SubscribeLogic] Find subscribe info failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find subscribe info failed: %v", err.Error())
	}

	// Find server list by user subscribe
	servers, err := l.getServers(userSubscribe)
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
			Password:     userSubscribe.UUID,
			ExpiredAt:    userSubscribe.ExpireTime,
			Download:     userSubscribe.Download,
			Upload:       userSubscribe.Upload,
			Traffic:      userSubscribe.Traffic,
			SubscribeURL: l.getSubscribeV2URL(),
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

	for _, format := range formats {
		if format == strings.ToLower(targetApp.OutputFormat) {
			l.ctx.Header("content-disposition", fmt.Sprintf("attachment;filename*=UTF-8''%s.%s", url.QueryEscape(l.svc.Config.Site.SiteName), format))
			l.ctx.Header("Content-Type", "application/octet-stream; charset=UTF-8")

		}
	}

	resp = &types.SubscribeResponse{
		Config: bytes,
		Header: fmt.Sprintf(
			"upload=%d;download=%d;total=%d;expire=%d",
			userSubscribe.Upload, userSubscribe.Download, userSubscribe.Traffic, userSubscribe.ExpireTime.Unix(),
		),
	}
	subscribeStatus = true
	return
}

func (l *SubscribeLogic) getSubscribeV2URL() string {

	uri := l.ctx.Request.RequestURI
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
	return fmt.Sprintf("https://%s%s", l.ctx.Request.Host, uri)
}

func (l *SubscribeLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.svc.UserModel.FindOneSubscribeByToken(l.ctx.Request.Context(), token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	//  Ignore expiration check
	//if userSub.Status > 1 {
	//	l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	//	return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
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
		ClientIP:        l.ctx.ClientIP(),
		UserSubscribeId: userSub.Id,
	}

	content, _ := subscribeLog.Marshal()

	err := l.svc.LogModel.Insert(l.ctx.Request.Context(), &log.SystemLog{
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: userSub.UserId, // log user id
		Date:     time.Now().Format(time.DateOnly),
		Content:  string(content),
	})
	if err != nil {
		l.Errorw("[Generate Subscribe]insert subscribe log error: %v", logger.Field("error", err.Error()))
	}
}

func (l *SubscribeLogic) getServers(userSub *user.Subscribe) ([]*node.Node, error) {
	if l.isSubscriptionExpired(userSub) {
		// 尝试获取过期节点组的节点
		expiredNodes, err := l.getExpiredGroupNodes(userSub)
		if err != nil {
			l.Errorw("[Generate Subscribe]get expired group nodes error", logger.Field("error", err.Error()))
			return l.createExpiredServers(), nil
		}
		// 如果有符合条件的过期节点组节点，返回它们
		if len(expiredNodes) > 0 {
			l.Debugf("[Generate Subscribe]user %d can use expired node group, nodes count: %d", userSub.UserId, len(expiredNodes))
			return expiredNodes, nil
		}
		// 否则返回假的过期节点
		l.Debugf("[Generate Subscribe]user %d cannot use expired node group, return fake expired nodes", userSub.UserId)
		return l.createExpiredServers(), nil
	}

	subDetails, err := l.svc.SubscribeModel.FindOne(l.ctx.Request.Context(), userSub.SubscribeId)
	if err != nil {
		l.Errorw("[Generate Subscribe]find subscribe details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe details error: %v", err.Error())
	}

	// 判断是否使用分组模式
	isGroupMode := l.isGroupEnabled()

	if isGroupMode {
		// === 分组模式：使用 node_group_id 获取节点 ===
		// 按优先级获取 node_group_id：user_subscribe.node_group_id > subscribe.node_group_id > subscribe.node_group_ids[0]
		nodeGroupId := int64(0)
		source := ""

		// 优先级1: user_subscribe.node_group_id
		if userSub.NodeGroupId != 0 {
			nodeGroupId = userSub.NodeGroupId
			source = "user_subscribe.node_group_id"
		} else {
			// 优先级2 & 3: 从 subscribe 表获取
			if subDetails.NodeGroupId != 0 {
				nodeGroupId = subDetails.NodeGroupId
				source = "subscribe.node_group_id"
			} else if len(subDetails.NodeGroupIds) > 0 {
				// 优先级3: subscribe.node_group_ids[0]
				nodeGroupId = subDetails.NodeGroupIds[0]
				source = "subscribe.node_group_ids[0]"
			}
		}

		l.Debugf("[Generate Subscribe]group mode, using %s: %v", source, nodeGroupId)

		var currentNodeGroup *group.NodeGroup
		if nodeGroupId > 0 {
			currentNodeGroup = l.getAccessibleNodeGroup(nodeGroupId, group.NodeGroupAccessSubscribe)
			if currentNodeGroup == nil {
				l.Debugf("[Generate Subscribe]node group %d from %s is not accessible for subscribe output", nodeGroupId, source)
				nodeGroupId = 0
			}
		}

		// 根据 node_group_id 获取节点
		enable := true

		// 1. 获取分组节点
		var groupNodes []*node.Node
		if nodeGroupId > 0 {
			params := &node.FilterNodeParams{
				Page:         0,
				Size:         1000,
				NodeGroupIds: []int64{nodeGroupId},
				Enabled:      &enable,
				Preload:      true,
			}
			_, groupNodes, err = l.svc.NodeModel.FilterNodeList(l.ctx.Request.Context(), params)

			if err != nil {
				l.Errorw("[Generate Subscribe]filter nodes by group error", logger.Field("error", err.Error()))
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "filter nodes by group error: %v", err.Error())
			}
			l.Debugf("[Generate Subscribe]found %d nodes for node_group_id=%d", len(groupNodes), nodeGroupId)
		}

		// 2. 获取公共节点（NodeGroupIds 为空的节点）
		_, allNodes, err := l.svc.NodeModel.FilterNodeList(l.ctx.Request.Context(), &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			Enabled: &enable,
			Preload: true,
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

		// 3. 合并分组节点和公共节点
		nodesMap := make(map[int64]*node.Node)
		for _, n := range groupNodes {
			nodesMap[n.Id] = n
		}
		for _, n := range publicNodes {
			if _, exists := nodesMap[n.Id]; !exists {
				nodesMap[n.Id] = n
			}
		}

		// 转换为切片
		var result []*node.Node
		for _, n := range nodesMap {
			result = append(result, n)
		}

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
	_, nodes, err = l.svc.NodeModel.FilterNodeList(l.ctx.Request.Context(), &node.FilterNodeParams{
		Page:    1,
		Size:    1000,
		NodeId:  nodeIds,
		Tag:     tool.RemoveDuplicateElements(tags...),
		Preload: true,
		Enabled: &enable,
	})

	if err != nil {
		l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
	}

	l.Debugf("[Generate Subscribe]found %d nodes in tag mode", len(nodes))
	return nodes, nil
}

func (l *SubscribeLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < time.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

func (l *SubscribeLogic) createExpiredServers() []*node.Node {
	enable := true
	host := l.getFirstHostLine()

	return []*node.Node{
		{
			Name:    "Subscribe Expired",
			Tags:    "",
			Port:    18080,
			Address: "127.0.0.1",
			Server: &node.Server{
				Id:        1,
				Name:      "Subscribe Expired",
				Protocols: "[{\"type\":\"shadowsocks\",\"cipher\":\"aes-256-gcm\",\"port\":1}]",
			},
			Protocol: "shadowsocks",
			Enabled:  &enable,
		},
		{
			Name:    host,
			Tags:    "",
			Port:    18080,
			Address: "127.0.0.1",
			Server: &node.Server{
				Id:        1,
				Name:      "Subscribe Expired",
				Protocols: "[{\"type\":\"shadowsocks\",\"cipher\":\"aes-256-gcm\",\"port\":1}]",
			},
			Protocol: "shadowsocks",
			Enabled:  &enable,
		},
	}
}

func (l *SubscribeLogic) getFirstHostLine() string {
	host := l.svc.Config.Host
	lines := strings.Split(host, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return host
}

// isGroupEnabled 判断分组功能是否启用
func (l *SubscribeLogic) isGroupEnabled() bool {
	var value string
	err := l.svc.DB.Table("system").
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
	err := l.svc.DB.Where("is_expired_group = ?", true).First(&expiredGroup).Error
	if err != nil {
		l.Debugw("[SubscribeLogic]no expired node group configured", logger.Field("error", err.Error()))
		return nil, err
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
	_, nodes, err := l.svc.NodeModel.FilterNodeList(l.ctx.Request.Context(), &node.FilterNodeParams{
		Page:         0,
		Size:         1000,
		NodeGroupIds: []int64{expiredGroup.Id},
		Enabled:      &enable,
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

func (l *SubscribeLogic) getAccessibleNodeGroup(nodeGroupId int64, accessType string) *group.NodeGroup {
	if nodeGroupId == 0 {
		return nil
	}

	var nodeGroup group.NodeGroup
	if err := l.svc.DB.Select("id, name, group_type").Where("id = ?", nodeGroupId).First(&nodeGroup).Error; err != nil {
		l.Infow("[Generate Subscribe]node group not found", logger.Field("nodeGroupId", nodeGroupId), logger.Field("error", err.Error()))
		return nil
	}

	if !group.IsNodeGroupTypeAccessible(nodeGroup.Type, accessType) {
		return nil
	}

	nodeGroup.Type = group.MustNodeGroupType(nodeGroup.Type)
	return &nodeGroup
}
