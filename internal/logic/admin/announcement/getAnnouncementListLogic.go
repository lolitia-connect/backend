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

type GetAnnouncementListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get announcement list
func NewGetAnnouncementListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAnnouncementListLogic {
	return &GetAnnouncementListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAnnouncementListLogic) GetAnnouncementList(req *types.GetAnnouncementListRequest) (resp *types.GetAnnouncementListResponse, err error) {
	size := int(req.Size)
	if size == 0 {
		size = 10
	}
	query := l.svcCtx.Ent.Announcement.Query()
	if req.Show != nil {
		query = query.Where(entannouncement.Show(*req.Show))
	}
	if req.Pinned != nil {
		query = query.Where(entannouncement.Pinned(*req.Pinned))
	}
	if req.Popup != nil {
		query = query.Where(entannouncement.Popup(*req.Popup))
	}
	if req.Search != "" {
		query = query.Where(entannouncement.Or(entannouncement.TitleContains(req.Search), entannouncement.ContentContains(req.Search)))
	}
	total, err := query.Count(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	list, err := query.Offset((int(req.Page) - 1) * size).Limit(size).All(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAnnouncementListByPage error: %v", err.Error())
	}
	resp = &types.GetAnnouncementListResponse{}
	resp.Total = int64(total)
	resp.List = make([]types.Announcement, 0)
	tool.DeepCopy(&resp.List, list)
	return
}
