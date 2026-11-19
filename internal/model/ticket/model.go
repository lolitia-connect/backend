package ticket

import (
	"context"
	"math"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entticket "github.com/perfect-panel/server/ent/ticket"
	entticketfollow "github.com/perfect-panel/server/ent/ticketfollow"
	"github.com/perfect-panel/server/pkg/xerr"
)

var cacheTicketDetailPrefix = "cache:ticket:detail:"

type Details struct {
	Id          int64
	Title       string
	Description string
	UserId      int64
	Status      uint8
	Follows     []Follow
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type customTicketLogicModel interface {
	QueryTicketDetail(ctx context.Context, id int64) (*Details, error)
	InsertTicketFollow(ctx context.Context, data *Follow) error
	Reply(ctx context.Context, data *Follow, userID int64) error
	QueryTicketList(ctx context.Context, page, size int, userId int64, status *uint8, search string) (int64, []*Ticket, error)
	UpdateTicketStatus(ctx context.Context, id, userId int64, status uint8) error
	QueryWaitReplyTotal(ctx context.Context) (int64, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customTicketModel{
		defaultTicketModel: newTicketModel(conn),
	}
}

// QueryTicketDetail returns the ticket details.
func (m *customTicketModel) QueryTicketDetail(ctx context.Context, id int64) (*Details, error) {
	ticket, err := m.db.Ticket.Query().Where(entticket.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	follows, err := m.db.TicketFollow.Query().Where(entticketfollow.TicketID(id)).All(ctx)
	if err != nil {
		return nil, err
	}
	data := &Details{
		Id:          ticket.ID,
		Title:       ticket.Title,
		Description: ticket.Description,
		UserId:      ticket.UserID,
		Status:      ticket.Status,
		CreatedAt:   ticket.CreatedAt,
		UpdatedAt:   ticket.UpdatedAt,
		Follows:     make([]Follow, 0, len(follows)),
	}
	for _, follow := range follows {
		data.Follows = append(data.Follows, Follow{
			Id:        follow.ID,
			TicketId:  follow.TicketID,
			From:      follow.From,
			Type:      follow.Type,
			Content:   follow.Content,
			CreatedAt: follow.CreatedAt,
		})
	}
	return data, nil
}

// InsertTicketFollow inserts a follow record.
func (m *customTicketModel) InsertTicketFollow(ctx context.Context, data *Follow) error {
	if err := ValidateFollow(data); err != nil {
		return err
	}
	created, err := m.db.TicketFollow.Create().
		SetTicketID(data.TicketId).
		SetFrom(data.From).
		SetType(data.Type).
		SetContent(data.Content).
		Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.TicketId = created.TicketID
	data.From = created.From
	data.Type = created.Type
	data.Content = created.Content
	data.CreatedAt = created.CreatedAt
	return nil
}

func (m *customTicketModel) Reply(ctx context.Context, data *Follow, userID int64) error {
	if data == nil {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	if userID > 0 {
		data.From = "User"
		if data.Type == 0 {
			data.Type = FollowText
		}
	}
	if err := ValidateFollow(data); err != nil {
		return err
	}
	transaction, err := m.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	client := transaction.Client()
	query := client.Ticket.Query().Where(entticket.ID(data.TicketId))
	if userID > 0 {
		query.Where(entticket.UserID(userID))
	}
	if m.db.Driver().Dialect() != dialect.SQLite {
		query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	current, err := query.Only(ctx)
	if err != nil {
		return err
	}
	status := uint8(Waiting)
	if userID > 0 {
		status = Pending
	} else if current.Status == Closed {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	created, err := client.TicketFollow.Create().SetTicketID(data.TicketId).SetFrom(data.From).SetType(data.Type).SetContent(data.Content).Save(ctx)
	if err != nil {
		return err
	}
	if err := client.Ticket.UpdateOneID(current.ID).SetStatus(status).Exec(ctx); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	data.Id, data.CreatedAt = created.ID, created.CreatedAt
	return nil
}

// QueryTicketList returns the ticket list.
func (m *customTicketModel) QueryTicketList(ctx context.Context, page, size int, userId int64, status *uint8, search string) (int64, []*Ticket, error) {
	page = max(page, 1)
	if size <= 0 {
		size = 20
	}
	size = min(size, 100)
	if page-1 > math.MaxInt/size {
		return 0, nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	query := m.db.Ticket.Query()
	if userId > 0 {
		query = query.Where(entticket.UserID(userId))
	}
	if status != nil {
		query = query.Where(entticket.Status(*status))
	} else {
		query = query.Where(entticket.StatusNEQ(Closed))
	}
	if search != "" {
		query = query.Where(entticket.Or(entticket.TitleContains(search), entticket.DescriptionContains(search)))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Order(ent.Desc(entticket.FieldID)).Limit(size).Offset((page - 1) * size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	data := make([]*Ticket, 0, len(items))
	for _, item := range items {
		data = append(data, ticketFromEnt(item))
	}
	return int64(total), data, nil
}

// UpdateTicketStatus updates the ticket status.
func (m *customTicketModel) UpdateTicketStatus(ctx context.Context, id, userId int64, status uint8) error {
	if id <= 0 || status < Pending || status > Closed || (userId > 0 && status != Closed) {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	query := m.db.Ticket.Update().Where(entticket.ID(id))
	if userId > 0 {
		query = query.Where(entticket.UserID(userId))
	}
	changed, err := query.SetStatus(status).Save(ctx)
	if err == nil && changed == 0 {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	return err
}

// QueryWaitReplyTotal returns the total number of tickets that are waiting for a reply.
func (m *customTicketModel) QueryWaitReplyTotal(ctx context.Context) (int64, error) {
	total, err := m.db.Ticket.Query().Where(entticket.Status(Pending)).Count(ctx)
	return int64(total), err
}
