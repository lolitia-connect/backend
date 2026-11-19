package email

import (
	"context"
	"errors"
	"sync"

	"github.com/perfect-panel/server/internal/model/task"
	"go.uber.org/zap"
)

var (
	Manager *WorkerManager // 全局调度器实例
	once    sync.Once      // 确保 Scheduler 只被初始化一次
)

type WorkerManager struct {
	tasks   task.Model                   // task repository
	sender  Sender                       // 邮件发送器接口
	mutex   sync.RWMutex                 // 读写互斥锁，确保线程安全
	workers map[int64]*Worker            // 存储所有 Worker 实例
	cancels map[int64]context.CancelFunc // 存储每个 Worker 的取消函数
	gate    chan struct{}
}

func NewWorkerManager(tasks task.Model, sender Sender) *WorkerManager {
	once.Do(func() {
		Manager = &WorkerManager{
			tasks:   tasks,
			workers: make(map[int64]*Worker),
			cancels: make(map[int64]context.CancelFunc),
			sender:  sender,
			gate:    make(chan struct{}, 1),
		}
	})
	return Manager
}

// AddWorker 添加一个新的 Worker 实例
func (m *WorkerManager) AddWorker(id int64) {
	go func() {
		if err := m.RunWorker(context.Background(), id, m.tasks, m.sender); err != nil {
			zap.S().Errorw("Batch Send Email", "task_id", id, "error", err)
		}
	}()
}

func (m *WorkerManager) RunWorker(ctx context.Context, id int64, tasks task.Model, sender Sender, options ...WorkerOption) error {
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	worker := NewWorker(workerCtx, id, tasks, sender, options...)
	m.mutex.Lock()
	if _, exists := m.workers[id]; exists {
		m.mutex.Unlock()
		return errors.New("email campaign worker is already running")
	}
	m.workers[id], m.cancels[id] = worker, cancel
	m.mutex.Unlock()
	defer func() {
		m.mutex.Lock()
		delete(m.workers, id)
		delete(m.cancels, id)
		m.mutex.Unlock()
	}()
	return worker.Start()
}

// GetWorker 获取指定任务的 Worker 实例
func (m *WorkerManager) GetWorker(id int64) *Worker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if worker, exists := m.workers[id]; exists {
		return worker
	} else {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Worker not found"),
			zap.Any("task_id", id),
		)
		return nil
	}
}

// RemoveWorker 移除指定任务的 Worker 实例
func (m *WorkerManager) RemoveWorker(id int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, exists := m.workers[id]; exists {
		if cancelFunc, ok := m.cancels[id]; ok {
			cancelFunc() // 调用取消函数
		}
		zap.S().Info("Batch Send Email",
			zap.Any("message", "Removed worker"),
			zap.Any("task_id", id),
		)
	} else {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Worker not found for removal"),
			zap.Any("task_id", id),
		)
	}
}
