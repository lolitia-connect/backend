package ticket

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entticket "github.com/perfect-panel/server/ent/ticket"
)

var _ Model = (*customTicketModel)(nil)
var (
	cacheTicketIdPrefix = "cache:ticket:id:"
)

type (
	Model interface {
		ticketModel
		customTicketLogicModel
	}
	ticketModel interface {
		Insert(ctx context.Context, data *Ticket) error
		FindOne(ctx context.Context, id int64) (*Ticket, error)
		Update(ctx context.Context, data *Ticket) error
		Delete(ctx context.Context, id int64) error
	}

	customTicketModel struct {
		*defaultTicketModel
	}
	defaultTicketModel struct {
		db *ent.Client
	}
)

func newTicketModel(db *ent.Client) *defaultTicketModel {
	return &defaultTicketModel{
		db: db,
	}
}

func (m *defaultTicketModel) Insert(ctx context.Context, data *Ticket) error {
	created, err := m.db.Ticket.Create().
		SetTitle(data.Title).
		SetDescription(data.Description).
		SetUserID(data.UserId).
		SetStatus(data.Status).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultTicketModel) FindOne(ctx context.Context, id int64) (*Ticket, error) {
	data, err := m.db.Ticket.Query().Where(entticket.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return ticketFromEnt(data), nil
}

func (m *defaultTicketModel) Update(ctx context.Context, data *Ticket) error {
	updated, err := m.db.Ticket.UpdateOneID(data.Id).
		SetTitle(data.Title).
		SetDescription(data.Description).
		SetUserID(data.UserId).
		SetStatus(data.Status).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultTicketModel) Delete(ctx context.Context, id int64) error {
	err := m.db.Ticket.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func ticketFromEnt(data *ent.Ticket) *Ticket {
	if data == nil {
		return nil
	}
	var resp Ticket
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Ticket, src *ent.Ticket) {
	dst.Id = src.ID
	dst.Title = src.Title
	dst.Description = src.Description
	dst.UserId = src.UserID
	dst.Status = src.Status
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
