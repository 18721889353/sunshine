// Package cron 提供定时任务管理功能
package cron

import (
	"context"

	"github.com/18721889353/sunshine/pkg/gocron"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// 任务注册表
	taskRegistry = make(map[string]*gocron.Task)
)

// RegisterTask 注册任务
func RegisterTask(task *gocron.Task) {
	if _, exists := taskRegistry[task.Name]; exists {
		panic("duplicate task name: " + task.Name)
	}
	taskRegistry[task.Name] = task
}

// GetTasks 获取所有已注册的任务
func GetTasks() []*gocron.Task {
	tasks := make([]*gocron.Task, 0, len(taskRegistry))
	for _, task := range taskRegistry {
		tasks = append(tasks, task)
	}
	logger.InfoWithCtx(context.Background(), "GetTasks", logger.Int("count", len(tasks)))
	return tasks
}
