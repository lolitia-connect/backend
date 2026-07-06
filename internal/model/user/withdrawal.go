package user

import (
	"context"
)

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
