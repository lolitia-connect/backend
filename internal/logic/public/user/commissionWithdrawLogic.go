package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CommissionWithdrawLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Commission Withdraw
func NewCommissionWithdrawLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommissionWithdrawLogic {
	return &CommissionWithdrawLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CommissionWithdrawLogic) CommissionWithdraw(req *types.CommissionWithdrawRequest) (resp *types.WithdrawalLog, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	walletInfo, err := l.svcCtx.Store.Wallet().FindOne(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorf("Failed to read wallet for user %d: %v", u.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to read wallet for user %d: %v", u.Id, err)
	}
	if walletInfo.Commission < req.Amount {
		zap.S().Errorf("User %d has insufficient commission balance: %.2f, requested: %.2f", u.Id, float64(walletInfo.Commission)/100, float64(req.Amount)/100)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserCommissionNotEnough), "User %d has insufficient commission balance", u.Id)
	}

	// create withdrawal log
	// Use a negative amount to reflect the balance decrease, so that
	// SumAmountByTypeAndObjectID produces the correct net total.
	logInfo := log.Commission{
		Type:      log.CommissionTypeConvertBalance,
		Amount:    -req.Amount,
		Timestamp: time.Now().UnixMilli(),
	}
	b, err := logInfo.Marshal()

	if err != nil {
		l.Logger.Errorf("Failed to marshal commission log for user %d: %v", u.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to marshal commission log for user %d: %v", u.Id, err)
	}

	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Re-read inside the transaction so a concurrent credit is not lost by
		// writing back a stale snapshot.
		walletInfo, err = store.Wallet().FindOne(l.ctx, u.Id)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to read wallet for user %d: %v", u.Id, err)
		}
		if walletInfo.Commission < req.Amount {
			return errors.Wrapf(xerr.NewErrCode(xerr.UserCommissionNotEnough), "User %d has insufficient commission balance", u.Id)
		}
		walletInfo.Commission -= req.Amount
		if err = store.Wallet().UpdateCommission(l.ctx, walletInfo); err != nil {
			l.Logger.Errorf("Failed to update user %d commission balance: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update user %d commission balance: %v", u.Id, err)
		}

		if err = store.Log().Insert(l.ctx, &log.SystemLog{
			Type:      log.TypeCommission.Uint8(),
			Date:      time.Now().Format("2006-01-02"),
			ObjectID:  u.Id,
			Content:   string(b),
			CreatedAt: time.Now(),
		}); err != nil {
			l.Logger.Errorf("Failed to create commission log for user %d: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Failed to create commission log for user %d: %v", u.Id, err)
		}

		if err = store.User().InsertWithdrawal(l.ctx, &user.Withdrawal{
			UserId:  u.Id,
			Amount:  req.Amount,
			Content: req.Content,
			Status:  0,
			Reason:  "",
		}); err != nil {
			l.Logger.Errorf("Failed to create withdrawal log for user %d: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Failed to create withdrawal log for user %d: %v", u.Id, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &types.WithdrawalLog{
		UserId:    u.Id,
		Amount:    req.Amount,
		Content:   req.Content,
		Status:    0,
		Reason:    "",
		CreatedAt: time.Now().UnixMilli(),
	}, nil
}
