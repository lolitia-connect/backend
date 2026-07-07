package system

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetNodeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeConfigLogic {
	return &GetNodeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeConfigLogic) GetNodeConfig() (*types.NodeConfig, error) {
	// get server config from db
	configs, err := l.svcCtx.Store.System().GetNodeConfig(l.ctx)
	if err != nil {
		l.Logger.Errorw("[GetNodeConfigLogic] GetNodeConfig get server config error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetNodeConfig get server config error: %v", err.Error())
	}
	var dbConfig config.NodeDBConfig
	tool.SystemConfigSliceReflectToStruct(configs, &dbConfig)
	c := &types.NodeConfig{
		NodeSecret:             dbConfig.NodeSecret,
		NodePullInterval:       dbConfig.NodePullInterval,
		NodePushInterval:       dbConfig.NodePushInterval,
		IPStrategy:             dbConfig.IPStrategy,
		TrafficReportThreshold: dbConfig.TrafficReportThreshold,
	}

	if dbConfig.DNS != "" {
		var dns []types.NodeDNS
		err = json.Unmarshal([]byte(dbConfig.DNS), &dns)
		if err != nil {
			zap.S().Errorf("[Node] Unmarshal DNS config error: %s", err.Error())
			panic(err)
		}
		c.DNS = dns
	}
	if dbConfig.Block != "" {
		var block []string
		_ = json.Unmarshal([]byte(dbConfig.Block), &block)
		c.Block = tool.RemoveDuplicateElements(block...)
	}
	if dbConfig.Outbound != "" {
		var outbound []types.NodeOutbound
		err = json.Unmarshal([]byte(dbConfig.Outbound), &outbound)
		if err != nil {
			zap.S().Errorf("[Node] Unmarshal Outbound config error: %s", err.Error())
			panic(err)
		}
		c.Outbound = outbound
	}

	return c, nil
}
