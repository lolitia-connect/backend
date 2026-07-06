package ticket

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entticket "github.com/perfect-panel/server/ent/ticket"
	entticketfollow "github.com/perfect-panel/server/ent/ticketfollow"
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

// QueryTicketList returns the ticket list.
func (m *customTicketModel) QueryTicketList(ctx context.Context, page, size int, userId int64, status *uint8, search string) (int64, []*Ticket, error) {
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
	query := m.db.Ticket.Update().Where(entticket.ID(id))
	if userId > 0 {
		query = query.Where(entticket.UserID(userId))
	}
	_, err := query.SetStatus(status).Save(ctx)
	return err
}

// QueryWaitReplyTotal returns the total number of tickets that are waiting for a reply.
func (m *customTicketModel) QueryWaitReplyTotal(ctx context.Context) (int64, error) {
	total, err := m.db.Ticket.Query().Where(entticket.Status(Pending)).Count(ctx)
	return int64(total), err
}
