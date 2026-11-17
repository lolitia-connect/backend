package withdrawal

import (
	"context"
	"strings"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ReviewWithdrawalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	Logger *zap.SugaredLogger
}

func NewReviewWithdrawalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReviewWithdrawalLogic {
	return &ReviewWithdrawalLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: zap.S(),
	}
}

// ReviewWithdrawal records the administrator's decision on a pending
// withdrawal.
//
// A rejection returns the withdrawn amount to the user's commission and writes
// the matching commission log; an approval just closes the request. The whole
// transition runs in one transaction that opens the withdrawal row with a
// write lock and only accepts a row that is still pending, so two reviewers
// racing on the same request cannot both refund it.
func (l *ReviewWithdrawalLogic) ReviewWithdrawal(req *types.ReviewWithdrawalRequest) error {
	reason := strings.TrimSpace(req.Reason)
	switch req.Status {
	case types.WithdrawalStatusApproved:
		// An approval carries no reason: the column records why a request was
		// refused, and leaving a stale one behind would misreport the outcome.
		reason = ""
	case types.WithdrawalStatusRejected:
		if reason == "" {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "a rejection reason is required")
		}
	default:
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "withdrawal status must be approved or rejected")
	}

	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		withdrawal, err := store.User().FindWithdrawalForUpdate(l.ctx, req.Id)
		if err != nil {
			if ent.IsNotFound(err) {
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "withdrawal %d does not exist", req.Id)
			}
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to read withdrawal %d: %v", req.Id, err)
		}
		if withdrawal.Status != types.WithdrawalStatusPending {
			return errors.Wrapf(xerr.NewErrCode(xerr.WithdrawalAlreadyReviewed), "withdrawal %d was already reviewed", req.Id)
		}

		if req.Status == types.WithdrawalStatusRejected {
			if err := l.refundCommission(store, withdrawal); err != nil {
				return err
			}
		}

		updated, err := store.User().UpdateWithdrawalStatus(
			l.ctx, withdrawal.Id, types.WithdrawalStatusPending, req.Status, reason,
		)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update withdrawal %d: %v", withdrawal.Id, err)
		}
		if !updated {
			// The row changed under the lock, so the refund above must not
			// stand: fail the transaction and let the caller retry.
			return errors.Wrapf(xerr.NewErrCode(xerr.WithdrawalAlreadyReviewed), "withdrawal %d changed concurrently", withdrawal.Id)
		}
		return nil
	})
	return err
}

// refundCommission returns the withdrawn amount to the user's commission and
// records the movement. The commission may legitimately be negative here: a
// refund claws back commission the user had already withdrawn, so only a sum
// that does not fit an int64 is refused.
func (l *ReviewWithdrawalLogic) refundCommission(store repository.Store, withdrawal *user.Withdrawal) error {
	account, err := store.Wallet().FindOneForUpdate(l.ctx, withdrawal.UserId)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to read wallet for user %d: %v", withdrawal.UserId, err)
	}
	refunded, ok := addInt64(account.Commission, withdrawal.Amount)
	if !ok {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "withdrawal refund would overflow commission balance")
	}
	account.Commission = refunded
	if err := store.Wallet().UpdateCommission(l.ctx, account); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to refund withdrawal commission for user %d: %v", withdrawal.UserId, err)
	}

	entry := log.Commission{
		Type:      log.CommissionTypeWithdraw,
		Amount:    withdrawal.Amount,
		Balance:   account.Commission,
		Timestamp: time.Now().UnixMilli(),
	}
	if metadata, ok := requestmeta.From(l.ctx); ok {
		entry.Metadata = metadata
	}
	content, err := entry.Marshal()
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to marshal withdrawal refund log: %v", err)
	}
	if err := store.Log().Insert(l.ctx, &log.SystemLog{
		Type:      log.TypeCommission.Uint8(),
		Date:      time.Now().Format(time.DateOnly),
		ObjectID:  withdrawal.UserId,
		Content:   string(content),
		CreatedAt: time.Now(),
	}); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Failed to insert withdrawal refund log: %v", err)
	}
	return nil
}

// addInt64 is a + b, reporting false when the sum does not fit an int64,
// whatever the signs of the operands.
func addInt64(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}
