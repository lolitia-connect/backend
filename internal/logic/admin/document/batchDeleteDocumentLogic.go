package document

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteDocumentLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete document
func NewBatchDeleteDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteDocumentLogic {
	return &BatchDeleteDocumentLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteDocumentLogic) BatchDeleteDocument(req *types.BatchDeleteDocumentRequest) error {
	for _, id := range req.Ids {
		intId, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "invalid document id: %v", id)
		}
		if err := l.svcCtx.Store.Document().Delete(l.ctx, intId); err != nil {
			l.Logger.Errorw("[BatchDeleteDocument] Database Error", zap.Any("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "failed to delete document: %v", err.Error())
		}
	}
	return nil
}
