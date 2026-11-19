package node

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var ErrServerBusy = errors.New("server user authorization is refreshing")

type UserIndex struct {
	Generation string
	ExpiresAt  time.Time
	Limits     map[int64]int64
	IDs        map[int64]struct{}
}

type userIndexEntry struct {
	key   string
	index *UserIndex
}

type UserIndexCache struct {
	mu          sync.Mutex
	entries     map[string]*list.Element
	order       list.List
	users       int
	loads       singleflight.Group
	listLoads   singleflight.Group
	builds      chan struct{}
	waiting     chan struct{}
	listBuilds  chan struct{}
	listWaiting chan struct{}
	claims      chan struct{}
}

func NewUserIndexCache() *UserIndexCache {
	return &UserIndexCache{entries: make(map[string]*list.Element), builds: make(chan struct{}, 4), waiting: make(chan struct{}, 256), listBuilds: make(chan struct{}, 4), listWaiting: make(chan struct{}, 256), claims: make(chan struct{}, 64)}
}

func (cache *UserIndexCache) acquireBuild(ctx context.Context) error {
	return acquireUserListBuild(ctx, cache.builds, cache.waiting)
}

func acquireUserListBuild(ctx context.Context, active, waiting chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrServerBusy, err)
	}
	select {
	case active <- struct{}{}:
		return nil
	default:
	}
	select {
	case waiting <- struct{}{}:
		defer func() { <-waiting }()
	default:
		return ErrServerBusy
	}
	select {
	case active <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-active
			return errors.Join(ErrServerBusy, err)
		}
		return nil
	case <-ctx.Done():
		return errors.Join(ErrServerBusy, ctx.Err())
	}
}

func (cache *UserIndexCache) LoadUserList(ctx context.Context, key string, load func(context.Context) (any, error)) (any, error) {
	result := cache.listLoads.DoChan(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := acquireUserListBuild(loadCtx, cache.listBuilds, cache.listWaiting); err != nil {
			return nil, err
		}
		defer func() { <-cache.listBuilds }()
		return load(loadCtx)
	})
	select {
	case <-ctx.Done():
		return nil, errors.Join(ErrServerBusy, ctx.Err())
	case result := <-result:
		return result.Val, result.Err
	}
}

func (cache *UserIndexCache) AcquireClaim() bool {
	select {
	case cache.claims <- struct{}{}:
		return true
	default:
		return false
	}
}

func (cache *UserIndexCache) ReleaseClaim() { <-cache.claims }

func (cache *UserIndexCache) cached(key, generation string) *UserIndex {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[key]
	if entry == nil {
		return nil
	}
	index := entry.Value.(userIndexEntry).index
	if index.Generation != generation || !time.Now().Before(index.ExpiresAt) {
		cache.remove(entry)
		return nil
	}
	cache.order.MoveToFront(entry)
	return index
}

func (cache *UserIndexCache) remove(element *list.Element) {
	entry := element.Value.(userIndexEntry)
	delete(cache.entries, entry.key)
	cache.users -= len(entry.index.IDs)
	cache.order.Remove(element)
}

func (cache *UserIndexCache) put(key string, index *UserIndex) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if old := cache.entries[key]; old != nil {
		cache.remove(old)
	}
	for cache.order.Len() > 0 && (cache.order.Len() >= 256 || cache.users+len(index.IDs) > 1_000_000) {
		cache.remove(cache.order.Back())
	}
	cache.entries[key] = cache.order.PushFront(userIndexEntry{key: key, index: index})
	cache.users += len(index.IDs)
}

func (cache *UserIndexCache) Get(ctx context.Context, key string, generation func(context.Context) (string, error), load func(context.Context) (*UserIndex, error)) (*UserIndex, error) {
	for attempt := 0; attempt < 3; attempt++ {
		version, err := generation(ctx)
		if err != nil {
			return nil, err
		}
		if index := cache.cached(key, version); index != nil {
			return index, nil
		}
		result := cache.loads.DoChan(key, func() (any, error) {
			loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if err := cache.acquireBuild(loadCtx); err != nil {
				return nil, err
			}
			defer func() { <-cache.builds }()
			current, err := generation(loadCtx)
			if err != nil {
				return nil, err
			}
			if index := cache.cached(key, current); index != nil {
				return index, nil
			}
			index, err := load(loadCtx)
			if err != nil {
				return nil, err
			}
			current, err = generation(loadCtx)
			if err != nil {
				return nil, err
			}
			if current != index.Generation || !time.Now().Before(index.ExpiresAt) {
				return nil, ErrServerBusy
			}
			cache.put(key, index)
			return index, nil
		})
		select {
		case <-ctx.Done():
			return nil, errors.Join(ErrServerBusy, ctx.Err())
		case result := <-result:
			if result.Err != nil {
				return nil, result.Err
			}
			index := result.Val.(*UserIndex)
			current, err := generation(ctx)
			if err != nil {
				return nil, err
			}
			if current == index.Generation && time.Now().Before(index.ExpiresAt) {
				return index, nil
			}
		}
	}
	return nil, ErrServerBusy
}
