package application

import (
	"context"
	"time"

	"github.com/perfect-panel/server/adapter"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type PreviewSubscribeTemplateLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Preview Template
func NewPreviewSubscribeTemplateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreviewSubscribeTemplateLogic {
	return &PreviewSubscribeTemplateLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PreviewSubscribeTemplateLogic) PreviewSubscribeTemplate(req *types.PreviewSubscribeTemplateRequest) (resp *types.PreviewSubscribeTemplateResponse, err error) {
	enable := true
	_, servers, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    1,
		Size:    1000,
		Preload: true,
		Enabled: &enable,
	})
	if err != nil {
		l.Logger.Errorf("[PreviewSubscribeTemplateLogic] FindAllServer error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindAllServer error: %v", err.Error())
	}

	data, err := l.svcCtx.Store.Client().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("[PreviewSubscribeTemplateLogic] FindOne error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneClient error: %v", err.Error())
	}

	sub := adapter.NewAdapter(data.SubscribeTemplate, adapter.WithServers(servers),
		adapter.WithSiteName("PerfectPanel"),
		adapter.WithSubscribeName("Test Subscribe"),
		adapter.WithOutputFormat(data.OutputFormat),
		adapter.WithUserInfo(adapter.User{
			Password:     "test-password",
			ExpiredAt:    time.Now().AddDate(1, 0, 0),
			Download:     0,
			Upload:       0,
			Traffic:      1000,
			SubscribeURL: "https://example.com/subscribe",
		}))
	// Get client config
	a, err := sub.Client()
	if err != nil {
		l.Logger.Errorf("[PreviewSubscribeTemplateLogic] Client error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrMsg(err.Error()), "Client error: %v", err.Error())
	}
	bytes, err := a.Build()
	if err != nil {
		l.Logger.Errorf("[PreviewSubscribeTemplateLogic] Build error: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrMsg(err.Error()), "Build error: %v", err.Error())
	}
	return &types.PreviewSubscribeTemplateResponse{
		Template: string(bytes),
	}, nil
}
