package announcement

import (
	"context"

	entannouncement "github.com/perfect-panel/server/ent/announcement"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query announcement
func NewQueryAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryAnnouncementLogic {
	return &QueryAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryAnnouncementLogic) QueryAnnouncement(req *types.QueryAnnouncementRequest) (resp *types.QueryAnnouncementResponse, err error) {
	size := req.Size
	if size == 0 {
		size = 10
	}
	query := l.svcCtx.Ent.Announcement.Query().Where(entannouncement.Show(true))
	if req.Pinned != nil {
		query = query.Where(entannouncement.Pinned(*req.Pinned))
	}
	if req.Popup != nil {
		query = query.Where(entannouncement.Popup(*req.Popup))
	}
	total, err := query.Count(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	list, err := query.Offset((req.Page - 1) * size).Limit(size).All(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &types.QueryAnnouncementResponse{}
	resp.Total = int64(total)
	resp.List = make([]types.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
