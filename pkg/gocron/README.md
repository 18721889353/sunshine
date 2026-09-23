## gocron

定时任务调度库，封装 [cron v3](https://github.com/robfig/cron)。

<br>

### 使用示例

```go
package main

import (
    "fmt"
    "time"

    "github.com/18721889353/sunshine/pkg/gocron"
)

var task1 = func() {
	fmt.Println("这是 task1")
	fmt.Println("运行中的任务列表:", gocron.GetRunningTasks())
}

var taskOnce = func() {
	fmt.Println("这是 task2, 只执行一次")
	fmt.Println("运行中的任务列表:", gocron.GetRunningTasks())
}

func main() {
	err := gocron.Init(
		gocron.WithOnlyPrintError(true), // 仅打印错误日志
	)
	if err != nil {
		panic(err)
	}

	gocron.Run([]*gocron.Task{
		{
			Name:     "task1",
			TimeSpec: "@every 2s",
			Fn:       task1,
		},
		{
			Name:      "taskOnce",
			TimeSpec:  "@every 3s",
			Fn:        taskOnce,
			IsRunOnce: true, // 仅执行一次
		},
	}...)

	time.Sleep(time.Second * 10)

	// 停止 task1
	gocron.DeleteTask("task1")

	// 查看运行中的任务
	fmt.Println("运行中的任务列表:", gocron.GetRunningTasks())
}
```

### 暂停与恢复

支持运行时暂停和恢复所有定时任务（用于 `openCron` 热更新）：

```go
// 暂停所有任务
gocron.Pause()

// 恢复所有任务（重建调度器，重新注册已注册的任务）
gocron.Resume()

// 查询是否暂停
if gocron.IsPaused() {
    fmt.Println("调度器已暂停")
}
```

> 注意：`Pause` 会停止调度器，`Resume` 会重建调度器并重新注册所有任务。
