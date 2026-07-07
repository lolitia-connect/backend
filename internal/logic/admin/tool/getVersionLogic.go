package tool

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"go.uber.org/zap"
)

type GetVersionLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetVersionLogic Get Version
func NewGetVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetVersionLogic {
	return &GetVersionLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetVersionLogic) GetVersion() (resp *types.VersionResponse, err error) {
	version := constant.Version
	buildTime := constant.BuildTime

	// Normalize unknown values
	if version == "unknown version" {
		version = "unknown"
	}
	if buildTime == "unknown time" {
		buildTime = "unknown"
	}

	// Format version based on whether it starts with 'v'
	var formattedVersion string
	if len(version) > 0 && version[0] == 'v' {
		formattedVersion = fmt.Sprintf("%s(%s)", version[1:], buildTime)
	} else {
		formattedVersion = fmt.Sprintf("%s(%s) Develop", version, buildTime)
	}

	return &types.VersionResponse{
		Version: formattedVersion,
	}, nil
}
