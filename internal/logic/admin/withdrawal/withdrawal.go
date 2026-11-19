package withdrawal

import (
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
)

// Page defaults and bounds shared by the administrator's withdrawal list, so a
// single request cannot ask for the whole table.
const (
	DefaultWithdrawalPageSize = 20
	MaxWithdrawalPageSize     = 100
)

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultWithdrawalPageSize
	}
	if size > MaxWithdrawalPageSize {
		size = MaxWithdrawalPageSize
	}
	return page, size
}

// withdrawalToType projects a withdrawal row onto its API representation. The
// timestamps are milliseconds because that is what the frontend formats.
func withdrawalToType(item *user.Withdrawal) *types.WithdrawalLog {
	if item == nil {
		return &types.WithdrawalLog{}
	}
	return &types.WithdrawalLog{
		Id:        item.Id,
		UserId:    item.UserId,
		Amount:    item.Amount,
		Content:   item.Content,
		Status:    item.Status,
		Reason:    item.Reason,
		CreatedAt: item.CreatedAt.UnixMilli(),
		UpdatedAt: item.UpdatedAt.UnixMilli(),
	}
}
