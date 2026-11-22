package listorder

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/server"
	"github.com/perfect-panel/server/ent/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

type List string

const (
	Nodes    List = node.Table
	Servers  List = server.Table
	Products List = subscribe.Table
)

type row struct {
	ID   int64
	Name string
	Sort int64
}

func (list List) rows(ctx context.Context, db *ent.Client) ([]row, error) {
	// Acquire locks in ID order for both drag and name sorts. SQLite serializes writes itself.
	lock := func(s *sql.Selector) {
		if db.Driver().Dialect() != dialect.SQLite {
			s.ForUpdate()
		}
	}
	var rows []row
	var err error
	switch list {
	case Nodes:
		err = db.Node.Query().Where(lock).Order(node.ByID()).Select(node.FieldID, node.FieldName, node.FieldSort).Scan(ctx, &rows)
	case Servers:
		err = db.Server.Query().Where(lock).Order(server.ByID()).Select(server.FieldID, server.FieldName, server.FieldSort).Scan(ctx, &rows)
	case Products:
		err = db.Subscribe.Query().Where(lock).Order(subscribe.ByID()).Select(subscribe.FieldID, subscribe.FieldName, subscribe.FieldSort).Scan(ctx, &rows)
	default:
		return nil, fmt.Errorf("unknown sortable list %q", list)
	}
	return rows, err
}

func (list List) set(ctx context.Context, tx repository.Store, id, rank int64) error {
	switch list {
	case Nodes:
		return tx.Node().UpdateNodeSort(ctx, id, rank)
	case Servers:
		return tx.Node().UpdateServerSort(ctx, id, rank)
	case Products:
		return tx.Subscribe().UpdateSortValue(ctx, id, rank)
	default:
		return fmt.Errorf("unknown sortable list %q", list)
	}
}

func sortError(err error) error {
	var invalid *xerr.CodeError
	switch {
	case err == nil, errors.As(err, &invalid):
		return err
	case ent.IsNotFound(err):
		return xerr.NewErrCodeMsg(xerr.InvalidParams, "sorted item no longer exists")
	default:
		return fmt.Errorf("%w: %v", xerr.NewErrCode(xerr.DatabaseUpdateError), err)
	}
}

// Reset sets an exact sort ID for a single row. A batch permutes the submitted rows
// within their existing global slots, including when a filter hides intervening rows.
func (list List) Reset(ctx context.Context, store repository.Store, items []types.SortItem) error {
	if len(items) == 0 {
		return nil
	}
	selected := make(map[int64]bool, len(items))
	for _, item := range items {
		if item.Id <= 0 || item.Sort < 0 || item.Sort > math.MaxInt32 || selected[item.Id] {
			return xerr.NewErrCodeMsg(xerr.InvalidParams, "invalid sort ID or duplicate item")
		}
		selected[item.Id] = true
	}
	return sortError(store.InTx(ctx, func(tx repository.Store) error {
		rows, err := list.rows(ctx, tx.Ent())
		if err != nil {
			return err
		}
		slices.SortFunc(rows, func(a, b row) int {
			return cmp.Or(cmp.Compare(a.Sort, b.Sort), cmp.Compare(a.ID, b.ID))
		})
		if len(items) == 1 {
			item := items[0]
			selectedIndex := slices.IndexFunc(rows, func(r row) bool { return r.ID == item.Id })
			if selectedIndex < 0 {
				return xerr.NewErrCodeMsg(xerr.InvalidParams, "sorted item no longer exists")
			}
			selectedRow := rows[selectedIndex]
			rows = append(rows[:selectedIndex], rows[selectedIndex+1:]...)
			insertIndex := len(rows)
			for i, r := range rows {
				if r.Sort >= item.Sort {
					insertIndex = i
					break
				}
			}
			rows = append(rows, row{})
			copy(rows[insertIndex+1:], rows[insertIndex:])
			selectedRow.Sort = item.Sort
			rows[insertIndex] = selectedRow
			previousSort := int64(-1)
			for i := range rows {
				if rows[i].Sort <= previousSort {
					if previousSort == math.MaxInt32 {
						return xerr.NewErrCodeMsg(xerr.InvalidParams, "sort ID range is exhausted")
					}
					rows[i].Sort = previousSort + 1
				}
				previousSort = rows[i].Sort
			}
			for _, item := range rows {
				if err := list.set(ctx, tx, item.ID, item.Sort); err != nil {
					return err
				}
			}
			return nil
		}
		if len(items) > 1 {
			requested := slices.Clone(items)
			slices.SortStableFunc(requested, func(a, b types.SortItem) int { return cmp.Compare(a.Sort, b.Sort) })
			selectedRows := make(map[int64]row, len(items))
			for _, r := range rows {
				if selected[r.ID] {
					selectedRows[r.ID] = r
				}
			}
			if len(selectedRows) != len(items) {
				return xerr.NewErrCodeMsg(xerr.InvalidParams, "sorted item no longer exists")
			}
			requestedIndex := 0
			for i, r := range rows {
				if !selected[r.ID] {
					continue
				}
				rows[i] = selectedRows[requested[requestedIndex].Id]
				requestedIndex++
			}
		}
		for i, item := range rows {
			rank := int64(i)
			if item.Sort != rank {
				if err := list.set(ctx, tx, item.ID, rank); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

func (list List) ByName(ctx context.Context, store repository.Store) error {
	return sortError(store.InTx(ctx, func(tx repository.Store) error {
		rows, err := list.rows(ctx, tx.Ent())
		if err != nil {
			return err
		}
		collator := collate.New(language.SimplifiedChinese, collate.Numeric)
		slices.SortFunc(rows, func(a, b row) int {
			return cmp.Or(collator.CompareString(a.Name, b.Name), strings.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		for i, r := range rows {
			if r.Sort != int64(i) {
				if err := list.set(ctx, tx, r.ID, int64(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}
