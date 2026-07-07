package repository

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/ads"
	"github.com/perfect-panel/server/internal/model/announcement"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/client"
	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/internal/model/document"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/model/redemption"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/system"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/model/traffic"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/redis/go-redis/v9"
)

type Store interface {
	Ent() *ent.Client
	Ads() ads.Model
	Announcement() announcement.Model
	Auth() auth.Model
	Client() client.Model
	Coupon() coupon.Model
	Document() document.Model
	Group() group.Model
	Log() log.Model
	Node() node.Model
	Order() order.Model
	Payment() payment.Model
	RedemptionCode() redemption.RedemptionCodeModel
	RedemptionRecord() redemption.RedemptionRecordModel
	Subscribe() subscribe.Model
	System() system.Model
	Task() task.Model
	Ticket() ticket.Model
	TrafficLog() traffic.Model
	User() user.Model

	InTx(ctx context.Context, fn func(store Store) error) error
}

var _ Store = (*StoreImpl)(nil)

type StoreImpl struct {
	ent   *ent.Client
	redis *redis.Client

	ads              ads.Model
	announcement     announcement.Model
	auth             auth.Model
	client           client.Model
	coupon           coupon.Model
	document         document.Model
	group            group.Model
	log              log.Model
	node             node.Model
	order            order.Model
	payment          payment.Model
	redemptionCode   redemption.RedemptionCodeModel
	redemptionRecord redemption.RedemptionRecordModel
	subscribe        subscribe.Model
	system           system.Model
	task             task.Model
	ticket           ticket.Model
	trafficLog       traffic.Model
	user             user.Model
}

func (s *StoreImpl) Ent() *ent.Client { return s.ent }

func NewStore(ec *ent.Client, rds *redis.Client) *StoreImpl {
	return &StoreImpl{
		ent:              ec,
		redis:            rds,
		ads:              ads.NewModel(ec),
		announcement:     announcement.NewModel(ec),
		auth:             auth.NewModel(ec),
		client:           client.NewSubscribeApplicationModel(ec),
		coupon:           coupon.NewModel(ec),
		document:         document.NewModel(ec),
		group:            group.NewModel(ec),
		log:              log.NewModel(ec),
		node:             node.NewModel(ec, rds),
		order:            order.NewModel(ec, rds),
		payment:          payment.NewModel(ec),
		redemptionCode:   redemption.NewRedemptionCodeModel(ec),
		redemptionRecord: redemption.NewRedemptionRecordModel(ec),
		subscribe:        subscribe.NewModel(ec, rds),
		system:           system.NewModel(ec),
		task:             task.NewModel(ec),
		ticket:           ticket.NewModel(ec),
		trafficLog:       traffic.NewModel(ec),
		user:             user.NewModel(ec, rds),
	}
}

func (s *StoreImpl) Ads() ads.Model {
	return s.ads
}

func (s *StoreImpl) Announcement() announcement.Model {
	return s.announcement
}

func (s *StoreImpl) Auth() auth.Model {
	return s.auth
}

func (s *StoreImpl) Client() client.Model {
	return s.client
}

func (s *StoreImpl) Coupon() coupon.Model {
	return s.coupon
}

func (s *StoreImpl) Document() document.Model {
	return s.document
}

func (s *StoreImpl) Group() group.Model {
	return s.group
}

func (s *StoreImpl) Log() log.Model {
	return s.log
}

func (s *StoreImpl) Node() node.Model {
	return s.node
}

func (s *StoreImpl) Order() order.Model {
	return s.order
}

func (s *StoreImpl) Payment() payment.Model {
	return s.payment
}

func (s *StoreImpl) RedemptionCode() redemption.RedemptionCodeModel {
	return s.redemptionCode
}

func (s *StoreImpl) RedemptionRecord() redemption.RedemptionRecordModel {
	return s.redemptionRecord
}

func (s *StoreImpl) Subscribe() subscribe.Model {
	return s.subscribe
}

func (s *StoreImpl) System() system.Model {
	return s.system
}

func (s *StoreImpl) Task() task.Model {
	return s.task
}

func (s *StoreImpl) Ticket() ticket.Model {
	return s.ticket
}

func (s *StoreImpl) TrafficLog() traffic.Model {
	return s.trafficLog
}

func (s *StoreImpl) User() user.Model {
	return s.user
}

func (s *StoreImpl) InTx(ctx context.Context, fn func(store Store) error) error {
	entTx, err := s.ent.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(NewStore(entTx.Client(), s.redis)); err != nil {
		if rbErr := entTx.Rollback(); rbErr != nil {
			return rbErr
		}
		return err
	}
	return entTx.Commit()
}
