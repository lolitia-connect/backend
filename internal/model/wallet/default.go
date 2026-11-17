package wallet

import (
	"context"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entuser "github.com/perfect-panel/server/ent/user"
	entwallet "github.com/perfect-panel/server/ent/userwallet"
)

type defaultWalletModel struct {
	db *ent.Client
}

// FindOneForUpdate opens the user row and then the wallet row with write locks,
// in that order, so every commission/balance movement in the process serialises
// on the same pair of rows. SQLite has no `SELECT ... FOR UPDATE`, so the
// clause is omitted there (the dialect the tests run on); MySQL and PostgreSQL,
// the supported production engines, always take the locks.
func (m *defaultWalletModel) FindOneForUpdate(ctx context.Context, userId int64) (*Wallet, error) {
	userQuery := m.db.User.Query().Where(entuser.ID(userId))
	if m.locking() {
		userQuery = userQuery.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	if _, err := userQuery.Only(ctx); err != nil {
		return nil, err
	}

	walletQuery := m.db.UserWallet.Query().Where(entwallet.UserID(userId))
	if m.locking() {
		walletQuery = walletQuery.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	row, err := walletQuery.Only(ctx)
	if ent.IsNotFound(err) {
		return &Wallet{UserId: userId}, nil
	}
	if err != nil {
		return nil, err
	}
	return entToWallet(row), nil
}

func (m *defaultWalletModel) locking() bool {
	return m.db.Driver().Dialect() != dialect.SQLite
}

func (m *defaultWalletModel) FindOne(ctx context.Context, userId int64) (*Wallet, error) {
	row, err := m.db.UserWallet.Query().Where(entwallet.UserID(userId)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return &Wallet{UserId: userId}, nil
		}
		return nil, err
	}
	return entToWallet(row), nil
}

func (m *defaultWalletModel) FindByUserIds(ctx context.Context, userIds []int64) (map[int64]*Wallet, error) {
	result := make(map[int64]*Wallet, len(userIds))
	if len(userIds) == 0 {
		return result, nil
	}
	rows, err := m.db.UserWallet.Query().Where(entwallet.UserIDIn(userIds...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UserID] = entToWallet(row)
	}
	return result, nil
}

func (m *defaultWalletModel) Insert(ctx context.Context, data *Wallet) error {
	created, err := walletCreate(m.db.UserWallet.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToWallet(created)
	return nil
}

func (m *defaultWalletModel) UpdateBalanceFields(ctx context.Context, data *Wallet) error {
	return m.save(ctx, data, func(id int64) (*ent.UserWallet, error) {
		return m.db.UserWallet.UpdateOneID(id).
			SetBalance(data.Balance).
			SetGiftAmount(data.GiftAmount).
			Save(ctx)
	})
}

// UpdateCommission persists only the commission column: commission credits
// and balance movements happen on different flows and must not overwrite
// each other.
func (m *defaultWalletModel) UpdateCommission(ctx context.Context, data *Wallet) error {
	return m.save(ctx, data, func(id int64) (*ent.UserWallet, error) {
		return m.db.UserWallet.UpdateOneID(id).
			SetCommission(data.Commission).
			Save(ctx)
	})
}

// save writes the wallet row. A row that was never seeded is inserted on the
// first write, so no account-creation path has to remember to open a wallet.
// When a concurrent writer wins the insert race the row is loaded and updated
// instead, which matters because these calls decide how much money moves.
func (m *defaultWalletModel) save(ctx context.Context, data *Wallet, update func(id int64) (*ent.UserWallet, error)) error {
	if data.Id == 0 {
		created, err := walletCreate(m.db.UserWallet.Create(), data).Save(ctx)
		if err == nil {
			*data = *entToWallet(created)
			return nil
		}
		if !ent.IsConstraintError(err) {
			return err
		}
		existing, qerr := m.db.UserWallet.Query().Where(entwallet.UserID(data.UserId)).Only(ctx)
		if qerr != nil {
			return qerr
		}
		data.Id = existing.ID
	}
	updated, err := update(data.Id)
	if err != nil {
		return err
	}
	*data = *entToWallet(updated)
	return nil
}

func entToWallet(e *ent.UserWallet) *Wallet {
	return &Wallet{
		Id:         e.ID,
		UserId:     e.UserID,
		Balance:    e.Balance,
		GiftAmount: e.GiftAmount,
		Commission: e.Commission,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

func walletCreate(c *ent.UserWalletCreate, data *Wallet) *ent.UserWalletCreate {
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	return c.SetUserID(data.UserId).
		SetBalance(data.Balance).
		SetGiftAmount(data.GiftAmount).
		SetCommission(data.Commission)
}
