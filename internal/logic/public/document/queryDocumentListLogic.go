package document

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryDocumentListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get document list
func NewQueryDocumentListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryDocumentListLogic {
	return &QueryDocumentListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryDocumentListLogic) QueryDocumentList() (resp *types.QueryDocumentListResponse, err error) {
	total, data, err := l.svcCtx.Store.Document().GetDocumentListByAll(l.ctx)
	if err != nil {
		l.Logger.Errorw("[QueryDocumentList] error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryDocumentList error: %v", err.Error())
	}
	resp = &types.QueryDocumentListResponse{
		Total: total,
		List:  make([]types.Document, 0),
	}
	for _, item := range data {
		resp.List = append(resp.List, types.Document{
			Id:        item.Id,
			Title:     item.Title,
			Tags:      tool.StringMergeAndRemoveDuplicates(item.Tags),
			UpdatedAt: item.UpdatedAt.UnixMilli(),
		})
	}
	return
}
