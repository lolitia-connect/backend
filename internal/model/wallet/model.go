package wallet

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
)

// Wallet is the billing-owned money record for one account. It replaces the
// balance, gift-amount and commission columns that used to live on the
// identity-owned user row.
type Wallet struct {
	Id         int64
	UserId     int64
	Balance    int64
	GiftAmount int64
	Commission int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Available is the spendable cash total: regular balance plus gift credit.
func (w *Wallet) Available() int64 {
	return w.Balance + w.GiftAmount
}

// Reserve splits an amount into the gift credit that pays for it and the cash
// remainder. Gift credit is spent before regular balance on every flow (order
// purchase, renewal, traffic reset and portal checkout), so the split lives
// here instead of being re-derived at each call site.
func (w *Wallet) Reserve(amount int64) (gift, remainder int64) {
	if amount <= 0 || w.GiftAmount <= 0 {
		return 0, amount
	}
	if w.GiftAmount >= amount {
		return amount, 0
	}
	return w.GiftAmount, amount - w.GiftAmount
}

// Model is the persistence surface for wallets. Every method returns a
// non-nil *Wallet; an account without a wallet row reads as zero values, so
// callers never have to special-case the first movement of money.
//
// Accounts are only ever soft-deleted, so there is no delete path here.
type Model interface {
	FindOne(ctx context.Context, userId int64) (*Wallet, error)
	FindByUserIds(ctx context.Context, userIds []int64) (map[int64]*Wallet, error)
	Insert(ctx context.Context, data *Wallet) error
	// UpdateBalanceFields persists the balance and gift columns. Commission
	// moves independently and goes through UpdateCommission. Both open the
	// row on first write.
	UpdateBalanceFields(ctx context.Context, data *Wallet) error
	UpdateCommission(ctx context.Context, data *Wallet) error
}

func NewModel(conn *ent.Client) Model {
	return &defaultWalletModel{db: conn}
}
