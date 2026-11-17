package user

import (
	"context"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entwithdrawal "github.com/perfect-panel/server/ent/userwithdrawal"
)

// Withdrawal statuses, matching the `user_withdrawal.status` column.
const (
	WithdrawalStatusPending uint8 = iota
	WithdrawalStatusApproved
	WithdrawalStatusRejected
)

// WithdrawalFilter selects a page of withdrawal requests. A nil UserId means
// every user (the administrator's queue); a nil Status means every status.
type WithdrawalFilter struct {
	UserId *int64
	Status *uint8
}

func (m *customUserModel) InsertWithdrawal(ctx context.Context, data *Withdrawal) error {
	c := m.db.UserWithdrawal.Create().SetUserID(data.UserId).SetAmount(data.Amount).SetContent(data.Content).SetStatus(data.Status).SetReason(data.Reason)
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	created, err := c.Save(ctx)
	if err != nil {
		return err
	}
	data.Id, data.CreatedAt, data.UpdatedAt = created.ID, created.CreatedAt, created.UpdatedAt
	return nil
}

func withdrawalQuery(db *ent.Client, filter *WithdrawalFilter) *ent.UserWithdrawalQuery {
	query := db.UserWithdrawal.Query()
	if filter == nil {
		return query
	}
	if filter.UserId != nil {
		query = query.Where(entwithdrawal.UserID(*filter.UserId))
	}
	if filter.Status != nil {
		query = query.Where(entwithdrawal.Status(*filter.Status))
	}
	return query
}

// QueryWithdrawalPageList pages withdrawal requests newest first, together
// with the total count of the same filter.
func (m *customUserModel) QueryWithdrawalPageList(ctx context.Context, filter *WithdrawalFilter, page, size int) ([]*Withdrawal, int64, error) {
	query := withdrawalQuery(m.db, filter)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := query.Order(entwithdrawal.ByID(entsql.OrderDesc())).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	list := make([]*Withdrawal, 0, len(rows))
	for _, row := range rows {
		list = append(list, entToWithdrawal(row))
	}
	return list, int64(total), nil
}

// FindWithdrawalForUpdate reads one withdrawal request under a row lock so
// that two reviewers cannot both act on the same pending request.
//
// SQLite has no `SELECT ... FOR UPDATE`, so the clause is omitted there (the
// dialect the tests run on); MySQL and PostgreSQL, the supported production
// engines, always take the lock.
func (m *customUserModel) FindWithdrawalForUpdate(ctx context.Context, id int64) (*Withdrawal, error) {
	query := m.db.UserWithdrawal.Query().Where(entwithdrawal.ID(id))
	if m.db.Driver().Dialect() != dialect.SQLite {
		query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	row, err := query.Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToWithdrawal(row), nil
}

// UpdateWithdrawalStatus applies a conditional status transition: it only
// succeeds when the stored status is still `from`, which makes the refund a
// rejection triggers happen at most once.
func (m *customUserModel) UpdateWithdrawalStatus(ctx context.Context, id int64, from, to uint8, reason string) (bool, error) {
	affected, err := m.db.UserWithdrawal.Update().
		Where(entwithdrawal.ID(id), entwithdrawal.Status(from)).
		SetStatus(to).
		SetReason(reason).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func entToWithdrawal(row *ent.UserWithdrawal) *Withdrawal {
	if row == nil {
		return nil
	}
	return &Withdrawal{
		Id:        row.ID,
		UserId:    row.UserID,
		Amount:    row.Amount,
		Content:   row.Content,
		Status:    row.Status,
		Reason:    row.Reason,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
