// Package app 负责优雅地启动和停止服务，使用 golang.org/x/sync/errgroup 确保多个服务同时启动。
package app

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/prof"
)

// IServer 定义了服务接口。
type IServer interface {
	Start() error   // 启动服务
	Stop() error    // 停止服务
	String() string // 返回服务的字符串表示
}

// Close 定义了一个关闭资源的函数类型。
type Close func() error

// App 管理一组服务和关闭函数。
type App struct {
	servers []IServer // 服务列表
	closes  []Close   // 关闭函数列表
}

// New 创建一个新的 App 实例，传入服务列表和关闭函数列表。
func New(servers []IServer, closes []Close) *App {
	return &App{
		servers: servers,
		closes:  closes,
	}
}

// Run 启动所有服务，并监控信号以停止应用。
func (a *App) Run() {

	writePIDToFile(fmt.Sprintf("启动时间:%v 进程id:%v", time.Now().Format(time.DateTime), strconv.Itoa(os.Getpid())))
	// 创建一个上下文，当任何一个 goroutine 返回错误时，该上下文将被取消。
	eg, ctx := errgroup.WithContext(context.Background())

	// 启动所有服务，每个服务在一个单独的 goroutine 中运行。
	for _, server := range a.servers {
		s := server
		eg.Go(func() error {
			fmt.Println(s.String()) // 打印服务名称
			writePIDToFile(s.String())
			return s.Start() // 启动服务
		})
	}

	// 监控操作系统信号和上下文取消信号，以停止应用。
	eg.Go(func() error {
		return a.watch(ctx)
	})

	// 等待所有 goroutine 完成。
	if err := eg.Wait(); err != nil {
		panic(err) // 如果有错误发生，触发 panic
	}
}

// watch 监控操作系统信号和上下文取消信号，如果任一信号被触发，则停止服务。
func (a *App) watch(ctx context.Context) error {
	sig := make(chan os.Signal, 1)                                                       // 创建一个信号通道
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTRAP) // 注册感兴趣的信号
	profile := prof.NewProfile()                                                         // 创建性能分析对象

	for {
		select {
		case <-ctx.Done(): // 服务错误
			_ = a.stop()     // 停止所有服务
			return ctx.Err() // 返回上下文的错误

		case sigType := <-sig: // 系统通知信号
			fmt.Printf("收到系统通知信号: %s\n", sigType.String()) // 打印接收到的信号

			writePIDToFile(fmt.Sprintf("收到系统通知信号: %s", sigType.String()))
			switch sigType {
			case syscall.SIGTRAP:
				profile.StartOrStop() // 开始或停止采样性能分析
			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
				if err := a.stop(); err != nil {
					return err // 如果停止服务时出错，返回错误
				}
				fmt.Println("应用已成功停止") // 打印停止成功的消息
				writePIDToFile(fmt.Sprintf("结束时间%v 应用已成功停止\n", time.Now().Format(time.DateTime)))
				return nil
			}
		}
	}
}

// stop 停止所有服务并释放资源。
func (a *App) stop() error {
	for _, closeFn := range a.closes {
		if err := closeFn(); err != nil {
			return err // 如果关闭资源时出错，返回错误
		}
	}
	return nil
}

// writePIDToFile 将启动关闭信息写入文件
func writePIDToFile(msg string) error {
	filePath := "sun.txt"
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logger.Warnf("Failed to open file %s: %v", filePath, err)
		}
	}(file)

	_, err = fmt.Fprintf(file, "%v\n", msg)
	if err != nil {
		return fmt.Errorf("failed to write msg to file %s: %v", filePath, err)
	}
	return nil
}
