package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ResetSortWithNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetSortWithNodeLogic Reset node sort
func NewResetSortWithNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetSortWithNodeLogic {
	return &ResetSortWithNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetSortWithNodeLogic) ResetSortWithNode(req *types.ResetSortRequest) error {
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		nodeStore := store.Node()

		// Build a set of requested IDs and their new sort values.
		reqMap := make(map[int64]int64, len(req.Sort))
		for _, item := range req.Sort {
			reqMap[item.Id] = item.Sort
		}

		// Fetch ALL current node sort values in a deterministic order.
		allSorts, err := nodeStore.QueryNodeSorts(l.ctx)
		if err != nil {
			return err
		}

		// Separate non-request items (preserve relative order) and
		// request items (in the order received from the frontend).
		nonReqItems := make([]node.SortItem, 0, len(allSorts))
		var reqItems []node.SortItem

		for _, item := range allSorts {
			if _, ok := reqMap[item.Id]; ok {
				reqItems = append(reqItems, node.SortItem{Id: item.Id, Sort: reqMap[item.Id]})
			} else {
				nonReqItems = append(nonReqItems, item)
			}
		}

		// If the frontend sent no items, nothing to do.
		if len(reqItems) == 0 {
			return nil
		}

		// Determine where the request items should be inserted.
		// Find the minimum original index of any request item in the global list
		// to preserve the page's position within the overall ordering.
		minIdx := len(allSorts)
		for i, item := range allSorts {
			if _, ok := reqMap[item.Id]; ok && i < minIdx {
				minIdx = i
			}
		}
		if minIdx > len(nonReqItems) {
			minIdx = len(nonReqItems)
		}

		// Build the new global ordering:
		// [items before page] + [page items in new order] + [items after page]
		newOrder := make([]node.SortItem, 0, len(allSorts))
		newOrder = append(newOrder, nonReqItems[:minIdx]...)
		newOrder = append(newOrder, reqItems...)
		newOrder = append(newOrder, nonReqItems[minIdx:]...)

		// Re-index all nodes with sequential sort values.
		for i, item := range newOrder {
			newSort := int64(i)
			if item.Sort != newSort {
				if err := nodeStore.UpdateNodeSort(l.ctx, item.Id, newSort); err != nil {
					l.Errorw("[NodeSort] Update Database Error: ", logger.Field("error", err.Error()), logger.Field("id", item.Id), logger.Field("sort", newSort))
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		l.Errorw("[NodeSort] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), err.Error())
	}
	return nil
}
