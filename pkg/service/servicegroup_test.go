package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/perfect-panel/server/pkg/proc"

	"github.com/stretchr/testify/assert"
)

type failedService struct {
	failure error
}

func (s failedService) Start()     {}
func (s failedService) Stop()      {}
func (s failedService) Run() error { return s.failure }

func TestGroupReturnsFailureAndStopsPeers(t *testing.T) {
	failure := errors.New("listener unavailable")
	group := NewServiceGroup()
	stopped := make(chan struct{})
	group.Add(WithStop(func() { close(stopped) }))
	group.Add(failedService{failure: failure})
	result := make(chan error, 1)
	go func() { result <- group.Run() }()
	select {
	case err := <-result:
		if !errors.Is(err, failure) {
			t.Fatalf("failure = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("service group did not propagate startup failure")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("peer service was not stopped")
	}
	group.Stop()
}

func TestGroupStopsPeersAfterStartupPanic(t *testing.T) {
	group := NewServiceGroup()
	stopped := false
	group.Add(WithStop(func() { stopped = true }))
	group.Add(WithStart(func() { panic("migration failed") }))
	if err := group.Run(); err == nil || !stopped {
		t.Fatalf("startup panic did not stop services: %v, stopped=%v", err, stopped)
	}
}

var (
	number = 1
	mutex  sync.Mutex
	done   = make(chan struct{})
)

func TestServiceGroup(t *testing.T) {
	multipliers := []int{2, 3, 5, 7}
	want := 1

	group := NewServiceGroup()
	for _, multiplier := range multipliers {
		want *= multiplier
		service := newMockedService(multiplier)
		group.Add(service)
	}

	go group.Start()

	for i := 0; i < len(multipliers); i++ {
		<-done
	}

	group.Stop()
	proc.Shutdown()

	mutex.Lock()
	defer mutex.Unlock()
	assert.Equal(t, want, number)
}

func TestServiceGroup_WithStart(t *testing.T) {
	multipliers := []int{2, 3, 5, 7}
	want := 1

	var wait sync.WaitGroup
	var lock sync.Mutex
	wait.Add(len(multipliers))
	group := NewServiceGroup()
	for _, multiplier := range multipliers {
		mul := multiplier
		group.Add(WithStart(func() {
			lock.Lock()
			want *= mul
			lock.Unlock()
			wait.Done()
		}))
	}

	go group.Start()
	wait.Wait()
	group.Stop()

	lock.Lock()
	defer lock.Unlock()
	assert.Equal(t, 210, want)
}

func TestServiceGroup_WithStarter(t *testing.T) {
	multipliers := []int{2, 3, 5, 7}
	want := 1

	var wait sync.WaitGroup
	var lock sync.Mutex
	wait.Add(len(multipliers))
	group := NewServiceGroup()
	for _, multiplier := range multipliers {
		mul := multiplier
		group.Add(WithStarter(mockedStarter{
			fn: func() {
				lock.Lock()
				want *= mul
				lock.Unlock()
				wait.Done()
			},
		}))
	}

	go group.Start()
	wait.Wait()
	group.Stop()

	lock.Lock()
	defer lock.Unlock()
	assert.Equal(t, 210, want)
}

type mockedStarter struct {
	fn func()
}

func (s mockedStarter) Start() {
	s.fn()
}

type mockedService struct {
	quit       chan struct{}
	multiplier int
}

func newMockedService(multiplier int) *mockedService {
	return &mockedService{
		quit:       make(chan struct{}),
		multiplier: multiplier,
	}
}

func (s *mockedService) Start() {
	mutex.Lock()
	number *= s.multiplier
	mutex.Unlock()
	done <- struct{}{}
	<-s.quit
}

func (s *mockedService) Stop() {
	close(s.quit)
}
