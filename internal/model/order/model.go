package order

import (
	"context"
	"sort"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entorder "github.com/perfect-panel/server/ent/order"
	entpayment "github.com/perfect-panel/server/ent/payment"
	"github.com/perfect-panel/server/ent/predicate"
	entsubscribe "github.com/perfect-panel/server/ent/subscribe"
	entuserauthmethod "github.com/perfect-panel/server/ent/userauthmethod"
	paymentmodel "github.com/perfect-panel/server/internal/model/payment"
	subscribemodel "github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
)

const userAuthMethodsTable = "user_auth_methods"

type customOrderLogicModel interface {
	UpdateOrderStatus(ctx context.Context, orderNo string, status uint8) error
	CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error)
	QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*Details, error)
	FindOneDetails(ctx context.Context, id int64) (*Details, error)
	FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*Details, error)
	QueryMonthlyOrders(ctx context.Context, date time.Time) (OrdersTotal, error)
	QueryDateOrders(ctx context.Context, date time.Time) (OrdersTotal, error)
	QueryTotalOrders(ctx context.Context) (OrdersTotal, error)
	QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryTotalUserCounts(ctx context.Context) (int64, int64, error)
	IsUserEligibleForNewOrder(ctx context.Context, userID int64) (bool, error)
	QueryDailyOrdersList(ctx context.Context, date time.Time) ([]OrdersTotalWithDate, error)
	QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]OrdersTotalWithDate, error)
}

func NewModel(conn *ent.Client, c *redis.Client) Model {
	return &customOrderModel{
		defaultOrderModel: newOrderModel(conn, c),
	}
}

func (m *customOrderModel) CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error) {
	count, err := m.db.Order.Query().Where(entorder.UserID(userID), entorder.Coupon(coupon)).Count(ctx)
	return int64(count), err
}

func (m *customOrderModel) QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*Details, error) {
	query := applyOrderListFilters(m.db.Order.Query(), status, user, subscribe, search)
	count, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := query.Order(entorder.ByID(entsql.OrderDesc())).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	details := entOrdersToDetails(list)
	if err = m.preloadDetails(ctx, details, false); err != nil {
		return 0, nil, err
	}
	return int64(count), details, nil
}

func applyOrderListFilters(query *ent.OrderQuery, status uint8, user, subscribe int64, search string) *ent.OrderQuery {
	if status > 0 {
		query = query.Where(entorder.Status(status))
	}
	if user > 0 {
		query = query.Where(entorder.UserID(user))
	}
	if subscribe > 0 {
		query = query.Where(entorder.SubscribeID(subscribe))
	}
	if search != "" {
		pattern := orm.LikePrefixPattern(search)
		if pattern != "" {
			query = query.Where(predicate.Order(func(s *entsql.Selector) {
				s.Where(entsql.Or(
					entsql.P(func(b *entsql.Builder) {
						b.Ident(s.C(entorder.FieldOrderNo)).WriteString(" LIKE ").Arg(pattern).WriteString(orm.LikeEscapeClause())
					}),
					entsql.P(func(b *entsql.Builder) {
						b.Ident(s.C(entorder.FieldTradeNo)).WriteString(" LIKE ").Arg(pattern).WriteString(orm.LikeEscapeClause())
					}),
					entsql.P(func(b *entsql.Builder) {
						b.Ident(s.C(entorder.FieldCoupon)).WriteString(" LIKE ").Arg(pattern).WriteString(orm.LikeEscapeClause())
					}),
					entsql.Exists(entsql.Select().From(entsql.Table(userAuthMethodsTable)).Where(entsql.And(
						entsql.ColumnsEQ(entsql.Table(userAuthMethodsTable).C(entuserauthmethod.FieldUserID), s.C(entorder.FieldUserID)),
						entsql.EQ(entsql.Table(userAuthMethodsTable).C(entuserauthmethod.FieldAuthType), "email"),
						entsql.P(func(b *entsql.Builder) {
							b.Ident(entsql.Table(userAuthMethodsTable).C(entuserauthmethod.FieldAuthIdentifier)).WriteString(" LIKE ").Arg(pattern).WriteString(orm.LikeEscapeClause())
						}),
					))),
				))
			}))
		}
	}
	return query
}

func (m *customOrderModel) UpdateOrderStatus(ctx context.Context, orderNo string, status uint8) error {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	if err = m.db.Order.Update().Where(entorder.OrderNo(orderNo)).SetStatus(status).Exec(ctx); err != nil {
		return err
	}
	orderInfo.Status = status
	return m.delCache(ctx, orderInfo)
}

func (m *customOrderModel) FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*Details, error) {
	data, err := m.db.Order.Query().Where(entorder.OrderNo(orderNo)).First(ctx)
	if err != nil {
		return nil, err
	}
	detail := entToDetails(data)
	return detail, m.preloadDetails(ctx, []*Details{detail}, false)
}

func (m *customOrderModel) FindOneDetails(ctx context.Context, id int64) (*Details, error) {
	data, err := m.db.Order.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	detail := entToDetails(data)
	return detail, m.preloadDetails(ctx, []*Details{detail}, true)
}

func (m *customOrderModel) QueryMonthlyOrders(ctx context.Context, date time.Time) (OrdersTotal, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	nextMonth := firstDay.AddDate(0, 1, 0)
	return m.queryOrdersTotal(ctx, entorder.CreatedAtGTE(firstDay), entorder.CreatedAtLT(nextMonth))
}

func (m *customOrderModel) QueryDateOrders(ctx context.Context, date time.Time) (OrdersTotal, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	nextDay := start.Add(24 * time.Hour)
	return m.queryOrdersTotal(ctx, entorder.CreatedAtGTE(start), entorder.CreatedAtLT(nextDay))
}

func (m *customOrderModel) QueryTotalOrders(ctx context.Context) (OrdersTotal, error) {
	return m.queryOrdersTotal(ctx)
}

func (m *customOrderModel) QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	nextMonth := firstDay.AddDate(0, 1, 0)
	return m.queryUserCounts(ctx, entorder.CreatedAtGTE(firstDay), entorder.CreatedAtLT(nextMonth))
}

func (m *customOrderModel) QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	nextDay := start.Add(24 * time.Hour)
	return m.queryUserCounts(ctx, entorder.CreatedAtGTE(start), entorder.CreatedAtLT(nextDay))
}

func (m *customOrderModel) QueryTotalUserCounts(ctx context.Context) (int64, int64, error) {
	return m.queryUserCounts(ctx)
}

func (m *customOrderModel) IsUserEligibleForNewOrder(ctx context.Context, userID int64) (bool, error) {
	count, err := m.db.Order.Query().Where(entorder.UserID(userID), entorder.StatusIn(2, 5)).Count(ctx)
	return count == 0, err
}

func (m *customOrderModel) QueryDailyOrdersList(ctx context.Context, date time.Time) ([]OrdersTotalWithDate, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	nextDay := date.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	return m.queryOrdersList(ctx, "day", firstDay, nextDay)
}

func (m *customOrderModel) QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]OrdersTotalWithDate, error) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, -5, 0)
	end := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, 1, 0)
	return m.queryOrdersList(ctx, "month", start, end)
}

func (m *customOrderModel) queryOrdersTotal(ctx context.Context, predicates ...predicate.Order) (OrdersTotal, error) {
	var result OrdersTotal
	list, err := m.paidNonBalanceOrders(ctx, predicates...).Select(entorder.FieldAmount, entorder.FieldIsNew).All(ctx)
	if err != nil {
		return result, err
	}
	for _, item := range list {
		result.AmountTotal += item.Amount
		if item.IsNew {
			result.NewOrderAmount += item.Amount
		} else {
			result.RenewalOrderAmount += item.Amount
		}
	}
	return result, nil
}

func (m *customOrderModel) queryUserCounts(ctx context.Context, predicates ...predicate.Order) (int64, int64, error) {
	list, err := m.paidNonBalanceOrders(ctx, predicates...).Select(entorder.FieldUserID, entorder.FieldIsNew).All(ctx)
	if err != nil {
		return 0, 0, err
	}
	newUsers := make(map[int64]struct{})
	renewalUsers := make(map[int64]struct{})
	for _, item := range list {
		if item.IsNew {
			newUsers[item.UserID] = struct{}{}
		} else {
			renewalUsers[item.UserID] = struct{}{}
		}
	}
	return int64(len(newUsers)), int64(len(renewalUsers)), nil
}

func (m *customOrderModel) queryOrdersList(ctx context.Context, bucket string, start, end time.Time) ([]OrdersTotalWithDate, error) {
	list, err := m.paidNonBalanceOrders(ctx, entorder.CreatedAtGTE(start), entorder.CreatedAtLT(end)).Select(entorder.FieldCreatedAt, entorder.FieldAmount, entorder.FieldIsNew).All(ctx)
	if err != nil {
		return nil, err
	}
	items := make(map[string]*OrdersTotalWithDate)
	for _, item := range list {
		key := formatOrderBucket(item.CreatedAt, bucket)
		total := items[key]
		if total == nil {
			total = &OrdersTotalWithDate{Date: key}
			items[key] = total
		}
		total.AmountTotal += item.Amount
		if item.IsNew {
			total.NewOrderAmount += item.Amount
		} else {
			total.RenewalOrderAmount += item.Amount
		}
	}
	results := make([]OrdersTotalWithDate, 0, len(items))
	for _, item := range items {
		results = append(results, *item)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Date < results[j].Date })
	return results, nil
}

func (m *customOrderModel) paidNonBalanceOrders(ctx context.Context, predicates ...predicate.Order) *ent.OrderQuery {
	return m.db.Order.Query().Where(append([]predicate.Order{entorder.StatusIn(2, 5), entorder.MethodNEQ("balance")}, predicates...)...)
}

func (m *customOrderModel) preloadDetails(ctx context.Context, details []*Details, includeSubOrders bool) error {
	paymentIds := make(map[int64]struct{})
	subscribeIds := make(map[int64]struct{})
	parentIds := make([]int64, 0, len(details))
	for _, item := range details {
		if item == nil {
			continue
		}
		if item.PaymentId > 0 {
			paymentIds[item.PaymentId] = struct{}{}
		}
		if item.SubscribeId > 0 {
			subscribeIds[item.SubscribeId] = struct{}{}
		}
		if includeSubOrders {
			parentIds = append(parentIds, item.Id)
		}
	}
	payments, err := m.queryPayments(ctx, paymentIds)
	if err != nil {
		return err
	}
	subscribes, err := m.querySubscribes(ctx, subscribeIds)
	if err != nil {
		return err
	}
	for _, item := range details {
		if item == nil {
			continue
		}
		item.Payment = payments[item.PaymentId]
		if item.Payment == nil && isBalancePayment(item) {
			item.Payment = defaultBalancePayment()
		}
		item.Subscribe = subscribes[item.SubscribeId]
	}
	if !includeSubOrders || len(parentIds) == 0 {
		return nil
	}
	subOrders, err := m.db.Order.Query().Where(entorder.ParentIDIn(parentIds...)).Order(entorder.ByID()).All(ctx)
	if err != nil {
		return err
	}
	byParent := make(map[int64][]*Order)
	for _, item := range subOrders {
		byParent[item.ParentID] = append(byParent[item.ParentID], entToOrder(item))
	}
	for _, item := range details {
		if item != nil {
			item.SubOrders = byParent[item.Id]
		}
	}
	return nil
}

func (m *customOrderModel) queryPayments(ctx context.Context, ids map[int64]struct{}) (map[int64]*paymentmodel.Payment, error) {
	result := make(map[int64]*paymentmodel.Payment)
	if len(ids) == 0 {
		return result, nil
	}
	values := mapKeys(ids)
	list, err := m.db.Payment.Query().Where(entpayment.IDIn(values...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		result[item.ID] = entToPayment(item)
	}
	return result, nil
}

func (m *customOrderModel) querySubscribes(ctx context.Context, ids map[int64]struct{}) (map[int64]*subscribemodel.Subscribe, error) {
	result := make(map[int64]*subscribemodel.Subscribe)
	if len(ids) == 0 {
		return result, nil
	}
	values := mapKeys(ids)
	list, err := m.db.Subscribe.Query().Where(entsubscribe.IDIn(values...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		result[item.ID] = entToSubscribe(item)
	}
	return result, nil
}

func mapKeys(values map[int64]struct{}) []int64 {
	keys := make([]int64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func formatOrderBucket(date time.Time, bucket string) string {
	if bucket == "month" {
		return date.Format("2006-01")
	}
	return date.Format("2006-01-02")
}
