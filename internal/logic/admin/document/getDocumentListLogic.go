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

type GetDocumentListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get document list
func NewGetDocumentListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDocumentListLogic {
	return &GetDocumentListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDocumentListLogic) GetDocumentList(req *types.GetDocumentListRequest) (resp *types.GetDocumentListResponse, err error) {
	total, data, err := l.svcCtx.Store.Document().QueryDocumentList(l.ctx, int(req.Page), int(req.Size), req.Tag, req.Search)
	if err != nil {
		l.Logger.Errorw("[GetDocumentList] Database Error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryDocumentList error: %v", err.Error())
	}
	resp = &types.GetDocumentListResponse{
		Total: total,
		List:  make([]types.Document, 0),
	}
	for _, v := range data {
		resp.List = append(resp.List, types.Document{
			Id:        v.Id,
			Title:     v.Title,
			Tags:      tool.StringMergeAndRemoveDuplicates(v.Tags),
			Content:   v.Content,
			Show:      *v.Show,
			CreatedAt: v.CreatedAt.UnixMilli(),
			UpdatedAt: v.UpdatedAt.UnixMilli(),
		})
	}
	return
}
