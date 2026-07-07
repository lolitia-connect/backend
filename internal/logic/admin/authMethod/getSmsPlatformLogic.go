package authMethod

import (
	"context"

	"github.com/perfect-panel/server/pkg/sms"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetSmsPlatformLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get sms support platform
func NewGetSmsPlatformLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSmsPlatformLogic {
	return &GetSmsPlatformLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSmsPlatformLogic) GetSmsPlatform() (resp *types.PlatformResponse, err error) {
	return &types.PlatformResponse{
		List: sms.GetSupportedPlatforms(),
	}, nil
}
