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

type QueryAnnouncementLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query announcement
func NewQueryAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryAnnouncementLogic {
	return &QueryAnnouncementLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryAnnouncementLogic) QueryAnnouncement(req *types.QueryAnnouncementRequest) (resp *types.QueryAnnouncementResponse, err error) {
	size := req.Size
	if size == 0 {
		size = 10
	}
	show := true
	total, list, err := l.svcCtx.Store.Announcement().GetAnnouncementListByPage(l.ctx, req.Page, size, model.Filter{Show: &show, Pinned: req.Pinned, Popup: req.Popup})
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &types.QueryAnnouncementResponse{}
	resp.Total = total
	resp.List = make([]types.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
