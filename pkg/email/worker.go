package email

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type ErrorInfo struct {
	Error string `json:"error"`
	Email string `json:"email"`
	Time  int64  `json:"time"`
}

type Worker struct {
	id     int64           // 任务ID
	tasks  task.Model      // task repository
	ctx    context.Context // 上下文
	sender Sender          // 邮件发送器接口
	status uint8           // 任务状态，0 表示未运行，1 表示运行中 2 表示已完成
}

func NewWorker(ctx context.Context, id int64, tasks task.Model, sender Sender) *Worker {
	return &Worker{
		id:     id,
		tasks:  tasks,
		ctx:    ctx,
		sender: sender,
	}
}

// GetID 获取Worker的任务ID
func (w *Worker) GetID() int64 {
	return w.id
}

// IsRunning 检查Worker是否正在运行
func (w *Worker) IsRunning() uint8 {
	return w.status
}

// Start 启动Worker，开始处理任务
func (w *Worker) Start() {
	// 检查并发限制
	limit.Lock()
	defer limit.Unlock()
	taskInfo, err := w.tasks.FindOne(w.ctx, w.id)
	if err != nil {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Failed to find task"),
			zap.Any("error", err.Error()),
			zap.Any("task_id", w.id),
		)
		return
	}
	if taskInfo.Status != 0 {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Task already completed or in progress"),
			zap.Any("task_id", w.id),
		)
		return
	}

	var scope task.EmailScope
	if err := json.Unmarshal([]byte(taskInfo.Scope), &scope); err != nil {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Failed to parse task scope"),
			zap.Any("error", err.Error()),
			zap.Any("task_id", w.id),
		)
		return
	}

	if len(scope.Recipients) == 0 && len(scope.Additional) == 0 {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "No recipients or additional emails provided"),
			zap.Any("task_id", w.id),
		)
		return
	}

	var content task.EmailContent
	if err := json.Unmarshal([]byte(taskInfo.Content), &content); err != nil {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Failed to parse task content"),
			zap.Any("error", err.Error()),
			zap.Any("task_id", w.id),
		)
		return
	}

	w.status = 1 // 设置状态为运行中
	var recipients []string
	// 解析收件人
	if len(scope.Recipients) > 0 {
		recipients = append(recipients, scope.Recipients...)
	}
	// 解析附加收件人
	if len(scope.Additional) > 0 {
		recipients = append(recipients, scope.Additional...)
	}
	// 去重和清理空字符串
	recipients = tool.RemoveDuplicateElements(recipients...)

	if len(recipients) == 0 {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "No valid recipients found"),
			zap.Any("task_id", w.id),
		)
		w.status = 2 // 设置状态为已完成
		return
	}

	// 设置发送间隔时间
	var intervalTime time.Duration
	if scope.Interval == 0 {
		intervalTime = 1 * time.Second
	} else {
		intervalTime = time.Duration(scope.Interval) * time.Second
	}

	var errors []ErrorInfo
	var count uint64
	for _, recipient := range recipients {
		select {
		case <-w.ctx.Done():
			zap.S().Info("Batch Send Email",
				zap.Any("message", "Worker stopped by context cancellation"),
				zap.Any("task_id", w.id),
			)
			return
		default:
		}
		if taskInfo.Status == 0 {
			taskInfo.Status = 1 // 1 表示任务进行中
		}

		if err := w.sender.Send([]string{recipient}, content.Subject, content.Content); err != nil {
			zap.S().Error("Batch Send Email",
				zap.Any("message", "Failed to send email"),
				zap.Any("error", err.Error()),
				zap.Any("recipient", recipient),
				zap.Any("task_id", w.id),
			)
			errors = append(errors, ErrorInfo{
				Error: err.Error(),
				Email: recipient,
				Time:  time.Now().Unix(),
			})
			text, _ := json.Marshal(errors)
			taskInfo.Errors = string(text)
		}
		count++
		taskInfo.Current = count
		if err := w.tasks.Update(w.ctx, taskInfo); err != nil {
			zap.S().Error("Batch Send Email",
				zap.Any("message", "Failed to update task progress"),
				zap.Any("error", err.Error()),
				zap.Any("task_id", w.id),
			)
			errors = append(errors, ErrorInfo{
				Error: err.Error(),
				Email: recipient,
				Time:  time.Now().Unix(),
			})
			w.status = 2 // 设置状态为已完成
		}
		time.Sleep(intervalTime)
	}
	taskInfo.Status = 2 // 2 表示任务已完成
	w.status = 2        // 设置状态为已完成

	if err := w.tasks.Update(w.ctx, taskInfo); err != nil {
		zap.S().Error("Batch Send Email",
			zap.Any("message", "Failed to finalize task"),
			zap.Any("error", err.Error()),
			zap.Any("task_id", w.id),
		)
	} else {
		zap.S().Info("Batch Send Email",
			zap.Any("message", "Task completed successfully"),
			zap.Any("task_id", w.id),
			zap.Any("total_sent", count),
		)
	}
}
