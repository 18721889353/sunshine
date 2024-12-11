package utils

import (
	"context"
	"fmt"
	"time"
)

// WaitPrinter 是一个等待打印器，用于在长时间操作期间显示等待提示信息。
type WaitPrinter struct {
	ctx            context.Context    // 上下文，用于控制等待打印器的生命周期
	cancel         context.CancelFunc // 取消函数，用于停止等待打印器
	printFrequency time.Duration      // 打印频率，控制提示信息更新的间隔时间
}

// NewWaitPrinter 创建一个新的 WaitPrinter 实例。
func NewWaitPrinter(interval time.Duration) *WaitPrinter {
	ctx, cancel := context.WithCancel(context.Background()) // 创建带有取消功能的上下文
	if interval < time.Millisecond*100 || interval > time.Second*5 {
		interval = time.Millisecond * 500 // 如果间隔时间不在合理范围内，设置默认值为 500 毫秒
	}
	return &WaitPrinter{
		ctx:            ctx,
		cancel:         cancel,
		printFrequency: interval,
	}
}

// LoopPrint 启动等待循环并打印运行提示信息。
func (p *WaitPrinter) LoopPrint(runningTip string) {
	if p == nil {
		return // 如果 WaitPrinter 实例为 nil，直接返回
	}
	go func() { // 使用 goroutine 异步执行等待循环
		symbols := []string{runningTip + ".", runningTip + "..", runningTip + "...", runningTip + "....", runningTip + ".....", runningTip + "......"} // 定义等待提示信息的动画符号
		index := 0                                                                                                                                     // 当前符号的索引
		fmt.Printf("\r%s", symbols[index])                                                                                                             // 打印初始提示信息

		ticker := time.NewTicker(p.printFrequency) // 创建一个定时器，按照指定频率触发
		defer ticker.Stop()                        // 确保在函数结束时停止定时器

		for {
			select {
			case <-p.ctx.Done(): // 如果上下文被取消，退出循环
				return
			case <-ticker.C: // 定时器触发时更新提示信息
				index++
				if index >= len(symbols) {
					index = 0            // 重置索引，循环播放动画
					p.clearCurrentLine() // 清除当前行
				}
				fmt.Printf("\r%s", symbols[index]) // 打印新的提示信息
			}
		}
	}()
}

// StopPrint 停止等待循环并打印提示信息。
func (p *WaitPrinter) StopPrint(tip string) {
	if p == nil {
		return // 如果 WaitPrinter 实例为 nil，直接返回
	}

	defer func() {
		if e := recover(); e != nil {
			fmt.Println(e) // 捕获并打印任何潜在的 panic 错误
		}
	}()

	p.cancel()           // 取消上下文，停止等待循环
	p.clearCurrentLine() // 清除当前行
	if tip == "" {
		return // 如果没有提示信息，直接返回
	}
	fmt.Println(tip) // 打印最终的提示信息
}

// clearCurrentLine 清除当前行的内容。
func (p *WaitPrinter) clearCurrentLine() {
	fmt.Print("\033[2K\r") // 使用 ANSI 转义序列清除当前行
}
