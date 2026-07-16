package commands

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/server"
)

// OpenUICommand run the sunshine ui service
func OpenUICommand() *cobra.Command {
	var (
		port         int
		sunshineAddr string
		isLog        bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "运行代码生成 UI 服务",
		Long:  "运行代码生成 UI 服务。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：运行代码生成 UI 服务
  # =====================================================================
  sunshine run \
    --port=24631 \
    --addr=http://192.168.1.10:24631 \
    --log=false


  # =====================================================================
  # 参数说明：
  #   --port   sunshine 服务监听端口（可选，默认 24631）
  #   --addr   前端页面请求 sunshine 服务的地址（可选）
  #   --log    是否启用服务日志（可选，默认 false）
`),
		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(_ *cobra.Command, _ []string) error {
			if sunshineAddr == "" {
				sunshineAddr = fmt.Sprintf("http://localhost:%d", port)
			} else {
				if err := checkSunshineAddr(sunshineAddr, port); err != nil {
					return err
				}
			}
			fmt.Printf("sunshine command ui service is running, port = %d, verson = %s, visit %s in your browser.\n\n", port, getVersion(), sunshineAddr)
			go func() {
				if openErr := open(sunshineAddr); openErr != nil {
					fmt.Printf("open browser error: %v\n", openErr)
				}
			}()
			server.RunHTTPServer(sunshineAddr, port, isLog)
			return nil
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 24631, "sunshine 服务监听端口")
	cmd.Flags().StringVarP(&sunshineAddr, "addr", "a", "", "前端页面请求 sunshine 服务的地址，格式: http://192.168.1.10:24631")
	cmd.Flags().BoolVarP(&isLog, "log", "l", false, "是否启用服务日志")
	return cmd
}

func open(visitURL string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}

	args = append(args, visitURL)
	return exec.Command(cmd, args...).Start()
}

func checkSunshineAddr(sunshineAddr string, port int) error {
	paramErr := errors.New("the addr parameter is invalid,  e.g. sunshine run --addr=http://192.168.1.10:24631")
	u, err := url.Parse(sunshineAddr)
	if err != nil {
		return paramErr
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return paramErr
	}

	ip := net.ParseIP(u.Hostname())
	if ip != nil {
		if u.Port() != strconv.Itoa(port) {
			return errors.New("the port parameter is invalid, e.g. sunshine run --port=8080 --addr=http://192.168.1.10:8080")
		}
	}

	return nil
}
