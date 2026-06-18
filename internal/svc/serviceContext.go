package svc

import (
	"context"
	"time"

	"github.com/perfect-panel/server/pkg/device"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/nodeMultiplier"
	"github.com/perfect-panel/server/pkg/orm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Redis        *redis.Client
	Config       config.Config
	Queue        *asynq.Client
	ExchangeRate float64
	GeoIP        *IPLocation
	Store        repository.Store

	//NodeCache   *cache.NodeCacheClient
	Restart               func() error
	TelegramBot           *tgbotapi.BotAPI
	NodeMultiplierManager *nodeMultiplier.Manager
	AuthLimiter           *limit.PeriodLimit
	DeviceManager         *device.DeviceManager
}

func NewServiceContext(c config.Config) *ServiceContext {
	// gorm initialize
	db, err := orm.ConnectMysql(orm.Mysql{
		Config: c.DatabaseConfig(),
	})

	if err != nil {
		panic(err.Error())
	}

	// IP location initialize
	geoIP, err := NewIPLocation("./cache/GeoLite2-City.mmdb")
	if err != nil {
		panic(err.Error())
	}

	rds := redis.NewClient(&redis.Options{
		Addr:            c.Redis.Host,
		Password:        c.Redis.Pass,
		DB:              c.Redis.DB,
		PoolSize:        c.Redis.PoolSize,                                  // 连接池大小：根据应用并发量调整，建议 100-500
		MinIdleConns:    c.Redis.MinIdleConns,                              // 最小空闲连接：保持一定数量的空闲连接，减少建立连接的开销
		MaxRetries:      c.Redis.MaxRetries,                                // 最大重试次数：网络抖动时自动重试
		PoolTimeout:     time.Second * time.Duration(c.Redis.PoolTimeout),  // 从连接池获取连接的超时时间
		ConnMaxIdleTime: time.Second * time.Duration(c.Redis.IdleTimeout),  // 空闲连接的超时时间，自动回收长时间空闲的连接
		ConnMaxLifetime: time.Second * time.Duration(c.Redis.MaxConnAge),   // 连接的最大生命周期，定期重建连接避免长时间使用的问题
		DialTimeout:     time.Second * time.Duration(c.Redis.DialTimeout),  // 建立新连接的超时时间
		ReadTimeout:     time.Second * time.Duration(c.Redis.ReadTimeout),  // 读操作超时时间
		WriteTimeout:    time.Second * time.Duration(c.Redis.WriteTimeout), // 写操作超时时间
	})
	err = rds.Ping(context.Background()).Err()
	if err != nil {
		panic(err.Error())
	}
	authLimiter := limit.NewPeriodLimit(86400, 15, rds, config.SendCountLimitKeyPrefix, limit.Align())
	store := repository.NewGormStore(db, rds)
	srv := &ServiceContext{
		Redis:        rds,
		Config:       c,
		Queue:        NewAsynqClient(c),
		ExchangeRate: 0,
		GeoIP:        geoIP,
		Store:        store,
		//NodeCache:   cache.NewNodeCacheClient(rds),
		AuthLimiter: authLimiter,
	}
	srv.DeviceManager = NewDeviceManager(srv)
	return srv

}
