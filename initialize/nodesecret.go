package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

// NodeSecret provisions a random node secret when the database does not carry
// one yet, so a fresh installation never serves the node API with a guessable
// credential. It has to run after Migrate, which seeds the empty row, and before
// Node, which copies the value into the runtime config.
//
// An installation still holding the legacy default is only reported, never
// rotated: every node was configured with that secret, so rotating it here would
// silently cut them off. The operator rotates it from the admin panel and
// reconfigures the nodes in the same window.
func NodeSecret(svcCtx *svc.ServiceContext) {
	zap.S().Debug("Node secret initialization")
	configs, err := svcCtx.Store.System().GetNodeConfig(context.Background())
	if err != nil {
		zap.S().Errorf("[NodeSecret] read node config error: %v", err.Error())
		panic(err)
	}
	var nodeConfig config.NodeDBConfig
	tool.SystemConfigSliceReflectToStruct(configs, &nodeConfig)

	switch nodeConfig.NodeSecret {
	case "":
		secret, err := tool.GenerateNodeSecret(tool.NodeSecretLength)
		if err != nil {
			zap.S().Errorf("[NodeSecret] generate error: %v", err.Error())
			panic(err)
		}
		if err := svcCtx.Store.System().UpdateValueByCategoryKey(context.Background(), "server", "NodeSecret", secret); err != nil {
			zap.S().Errorf("[NodeSecret] persist error: %v", err.Error())
			panic(err)
		}
		zap.S().Info("[NodeSecret] generated a random node secret, read it from the admin panel to configure nodes")
	case tool.LegacyDefaultNodeSecret:
		zap.S().Error("[NodeSecret] the node secret is still the well-known default, rotate it from the admin panel and reconfigure every node")
	}
}
