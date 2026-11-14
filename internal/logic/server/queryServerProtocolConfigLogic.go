package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/nodeconfig"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type QueryServerProtocolConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryServerProtocolConfigLogic Get Server Protocol Config
func NewQueryServerProtocolConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryServerProtocolConfigLogic {
	return &QueryServerProtocolConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryServerProtocolConfigLogic) QueryServerProtocolConfig(req *types.QueryServerConfigRequest) (resp *types.QueryServerConfigResponse, err error) {
	// find server
	data, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerID)
	if err != nil {
		l.Logger.Errorf("[GetServerProtocols] FindOneServer Error: %s", err.Error())
		return nil, err
	}

	// handler protocols
	var protocols []types.Protocol
	dst, err := data.UnmarshalProtocols()
	if err != nil {
		l.Logger.Errorf("[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
		return nil, err
	}
	dst = node.SanitizeProtocolsForNodeDistribution(dst)
	tool.DeepCopy(&protocols, dst)
	protocols = filterServerProtocols(protocols, req.Protocols)

	nodeValues := nodeconfig.GlobalValues(l.svcCtx.Config.Node)
	override, err := l.svcCtx.Store.Node().FindServerConfigOverride(l.ctx, req.ServerID)
	if err != nil {
		l.Logger.Errorf("[GetServerProtocols] FindServerConfigOverride Error: %s", err.Error())
		return nil, err
	}
	if override != nil {
		if err = nodeconfig.ApplyOverride(&nodeValues, override); err != nil {
			l.Logger.Errorf("[GetServerProtocols] ApplyOverride Error: %s", err.Error())
			return nil, err
		}
	}

	return &types.QueryServerConfigResponse{
		TrafficReportThreshold: l.svcCtx.Config.Node.TrafficReportThreshold,
		PushInterval:           l.svcCtx.Config.Node.NodePushInterval,
		PullInterval:           l.svcCtx.Config.Node.NodePullInterval,
		IPStrategy:             nodeValues.IPStrategy,
		DNS:                    nodeValues.DNS,
		Block:                  nodeValues.Block,
		Outbound:               nodeValues.Outbound,
		Protocols:              protocols,
		Total:                  int64(len(protocols)),
	}, nil
}

func filterServerProtocols(protocols []types.Protocol, requested []string) []types.Protocol {
	protocolSet := make(map[string]struct{}, len(requested))
	for _, selector := range requested {
		protocolSet[selector] = struct{}{}
	}

	var filtered []types.Protocol
	for _, protocol := range protocols {
		if !protocol.Enable {
			continue
		}
		if len(protocolSet) > 0 {
			_, matchesType := protocolSet[protocol.Type]
			_, matchesInstance := protocolSet[protocol.Type+":"+protocol.Id]
			if !matchesType && !matchesInstance {
				continue
			}
		}
		filtered = append(filtered, protocol)
	}
	return filtered
}
