package service

import (
	"fmt"
	"sync"

	"github.com/perfect-panel/server/pkg/proc"
	"github.com/perfect-panel/server/pkg/threading"
)

type (
	Runner interface {
		Run() error
	}

	// Starter is the interface wraps the Start method.
	Starter interface {
		Start()
	}

	// Stopper is the interface wraps the Stop method.
	Stopper interface {
		Stop()
	}

	// Service is the interface that groups Start and Stop methods.
	Service interface {
		Starter
		Stopper
	}

	// Group A ServiceGroup is a group of services.
	// Attention: the starting order of the added services is not guaranteed.
	Group struct {
		services []Service
		stopOnce func()
	}
)

// NewServiceGroup returns a ServiceGroup.
func NewServiceGroup() *Group {
	sg := new(Group)
	sg.stopOnce = Once(sg.doStop)
	return sg
}

// Add adds service into sg.
func (sg *Group) Add(service Service) {
	// push front, stop with reverse order.
	sg.services = append([]Service{service}, sg.services...)
}

// Start starts the ServiceGroup.
// There should not be any logic code after calling this method, because this method is a blocking one.
// Also, quitting this method will close the logx output.
func (sg *Group) Start() {
	if err := sg.Run(); err != nil {
		panic(err)
	}
}

func (sg *Group) Run() error {
	proc.AddShutdownListener(func() {
		sg.stopOnce()
	})

	return sg.doStart()
}

// Stop stops the ServiceGroup.
func (sg *Group) Stop() {
	sg.stopOnce()
}

func (sg *Group) doStart() error {
	routineGroup := threading.NewRoutineGroup()
	results := make(chan error, len(sg.services))

	for i := range sg.services {
		service := sg.services[i]
		routineGroup.Run(func() {
			defer func() {
				if failure := recover(); failure != nil {
					results <- fmt.Errorf("service startup failed: %v", failure)
				}
			}()
			if runner, ok := service.(Runner); ok {
				results <- runner.Run()
			} else {
				service.Start()
				results <- nil
			}
		})
	}

	var firstError error
	for range sg.services {
		if err := <-results; err != nil && firstError == nil {
			firstError = err
			sg.Stop()
		}
	}
	routineGroup.Wait()
	return firstError
}

func (sg *Group) doStop() {
	for _, service := range sg.services {
		service.Stop()
	}
}

// WithStart wraps a start func as a Service.
func WithStart(start func()) Service {
	return startOnlyService{
		start: start,
	}
}

func WithStop(stop func()) Service {
	return stopOnlyService{stop: stop}
}

// WithStarter wraps a Starter as a Service.
func WithStarter(start Starter) Service {
	return starterOnlyService{
		Starter: start,
	}
}

type (
	stopper struct{}

	startOnlyService struct {
		start func()
		stopper
	}

	starterOnlyService struct {
		Starter
		stopper
	}

	stopOnlyService struct {
		stop func()
	}
)

func (s stopOnlyService) Start() {}

func (s stopOnlyService) Stop() { s.stop() }

func (s stopper) Stop() {
}

func (s startOnlyService) Start() {
	s.start()
}

func Once(fn func()) func() {
	once := new(sync.Once)
	return func() {
		once.Do(fn)
	}
}
