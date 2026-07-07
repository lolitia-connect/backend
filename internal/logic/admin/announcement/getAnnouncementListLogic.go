package announcement

import (
	"context"

	model "github.com/perfect-panel/server/internal/model/announcement"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetAnnouncementListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get announcement list
func NewGetAnnouncementListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAnnouncementListLogic {
	return &GetAnnouncementListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAnnouncementListLogic) GetAnnouncementList(req *types.GetAnnouncementListRequest) (resp *types.GetAnnouncementListResponse, err error) {
	size := int(req.Size)
	if size == 0 {
		size = 10
	}
	total, list, err := l.svcCtx.Store.Announcement().GetAnnouncementListByPage(l.ctx, int(req.Page), size, model.Filter{Show: req.Show, Pinned: req.Pinned, Popup: req.Popup, Search: req.Search})
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &types.GetAnnouncementListResponse{}
	resp.Total = total
	resp.List = make([]types.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
