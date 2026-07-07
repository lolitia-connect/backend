package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryNodeTagLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryNodeTagLogic Query all node tags
func NewQueryNodeTagLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryNodeTagLogic {
	return &QueryNodeTagLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryNodeTagLogic) QueryNodeTag() (resp *types.QueryNodeTagResponse, err error) {

	nodeTags, err := l.svcCtx.Store.Node().QueryNodeTags(l.ctx)
	if err != nil {
		l.Logger.Errorw("[QueryNodeTag] Query Database Error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[QueryNodeTag] Query Database Error")
	}
	var tags []string
	for _, item := range nodeTags {
		tags = append(tags, strings.Split(item, ",")...)
	}

	return &types.QueryNodeTagResponse{
		Tags: tool.RemoveDuplicateElements(tags...),
	}, nil
}
