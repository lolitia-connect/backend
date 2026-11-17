package server

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/trafficagg"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

//goland:noinspection GoNameStartsWithPackageName
type ServerPushUserTrafficLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewServerPushUserTrafficLogic Push user Traffic
func NewServerPushUserTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServerPushUserTrafficLogic {
	return &ServerPushUserTrafficLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ServerPushUserTrafficLogic) ServerPushUserTraffic(req *types.ServerPushUserTrafficRequest) error {
	server, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil || server == nil {
		return fmt.Errorf("traffic server not found: %v", err)
	}
	logs := make([]trafficagg.Report, 0, len(req.Traffic))
	for _, entry := range req.Traffic {
		logs = append(logs, trafficagg.Report{SID: entry.SID, Upload: entry.Upload, Download: entry.Download})
	}
	return NewTrafficAggregator(l.svcCtx).AddReportAt(l.ctx, server, req.Protocol, req.ProtocolId, logs, time.Now())
}

func NewTrafficAggregator(service *svc.ServiceContext) *trafficagg.Aggregator {
	aggregator := &trafficagg.Aggregator{Store: service.Store, Redis: service.Redis}
	if service.NodeMultiplierManager != nil {
		aggregator.Multiplier = service.NodeMultiplierManager.GetMultiplier
	}
	aggregator.Served = func(ctx context.Context, serverID int64, protocol, protocolID string) (map[int64]struct{}, error) {
		return ServedSubscriptionIDs(ctx, service, serverID, protocol, protocolID)
	}
	return aggregator
}

func ServedSubscriptionIDs(ctx context.Context, service *svc.ServiceContext, serverID int64, protocol, protocolID string) (map[int64]struct{}, error) {
	index, err := servedUserIndex(ctx, service, serverID, protocol, protocolID)
	if err != nil {
		return nil, err
	}
	return index.IDs, nil
}

func servedUserIndex(ctx context.Context, service *svc.ServiceContext, serverID int64, protocol, protocolID string) (*node.UserIndex, error) {
	key := fmt.Sprintf("%d:%s:%s", serverID, protocol, protocolID)
	return service.NodeUserIndexes().Get(ctx, key, func(ctx context.Context) (string, error) {
		return node.ServerCacheGeneration(ctx, service.Redis, serverID)
	}, func(ctx context.Context) (*node.UserIndex, error) {
		return loadServedUserIndex(ctx, service, serverID, protocol, protocolID)
	})
}

func loadServedUserIndex(ctx context.Context, service *svc.ServiceContext, serverID int64, protocol, protocolID string) (*node.UserIndex, error) {
	version, err := node.ServerCacheGeneration(ctx, service.Redis, serverID)
	if err != nil {
		return nil, err
	}
	server, err := service.Store.Node().FindOneServer(ctx, serverID)
	if err != nil || server == nil {
		return nil, fmt.Errorf("server not found: %v", err)
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		return nil, err
	}
	found := false
	for _, instance := range protocols {
		if instance.Type == protocol && instance.Id == protocolID && instance.Enable {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("enabled protocol instance %s:%s not found", protocol, protocolID)
	}
	logic := NewGetServerUserListLogic(ctx, service, RequestMeta{})
	response, err := logic.GetServerUserList(&types.GetServerUserListRequest{ServerCommon: types.ServerCommon{ServerId: serverID, Protocol: protocol, ProtocolId: protocolID}})
	if err != nil {
		return nil, err
	}
	if logic.generation != version {
		return nil, node.ErrServerBusy
	}
	index := &node.UserIndex{Generation: version, ExpiresAt: logic.expiresAt, Limits: make(map[int64]int64), IDs: make(map[int64]struct{})}
	credentials := make(map[int64]string)
	limits := make(map[int64]int64)
	var ids []int64
	if response != nil {
		for _, entry := range response.Users {
			credentials[entry.Id] = entry.UUID
			limits[entry.Id] = entry.DeviceLimit
			ids = append(ids, entry.Id)
		}
	}
	for offset := 0; offset < len(ids); offset += 500 {
		subscriptions, err := service.Store.User().FindSubscribesByIds(ctx, ids[offset:min(offset+500, len(ids))])
		if err != nil {
			return nil, err
		}
		ownerIDs := make([]int64, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			if subscription != nil {
				ownerIDs = append(ownerIDs, subscription.UserId)
			}
		}
		owners, err := service.Store.User().FindUsersByIds(ctx, ownerIDs)
		if err != nil {
			return nil, err
		}
		enabled := make(map[int64]bool, len(owners))
		for _, owner := range owners {
			if owner != nil {
				enabled[owner.Id] = owner.DeletedAt == nil && owner.Enable != nil && *owner.Enable
			}
		}
		for _, subscription := range subscriptions {
			if subscription == nil || !enabled[subscription.UserId] || subscription.UUID == "" {
				continue
			}
			credential, err := node.ProtocolCredential(protocol, subscription.UUID)
			if err != nil {
				continue
			}
			if credential == credentials[subscription.Id] {
				index.IDs[subscription.Id] = struct{}{}
				index.Limits[subscription.Id] = limits[subscription.Id]
			}
		}
	}
	return index, nil
}
