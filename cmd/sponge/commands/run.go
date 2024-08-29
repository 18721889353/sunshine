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
		Short: "Run the sunshine UI service",
		Long: color.HiBlackString(`run the sunshine UI service.

Examples:
  # running ui service, local browser access only.
  sunshine run

  # running ui service, can be accessed from other host browsers.
  sunshine run -a http://your-host-ip:24631
`),
		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: func(cmd *cobra.Command, args []string) error {
			if sunshineAddr == "" {
				sunshineAddr = fmt.Sprintf("http://localhost:%d", port)
			} else {
				if err := checkSunshineAddr(sunshineAddr, port); err != nil {
					return err
				}
			}
			fmt.Printf("sunshine command ui service is running, port = %d, verson = %s, visit %s in your browser.\n\n", port, getVersion(), sunshineAddr)
			go func() {
				_ = open(sunshineAddr)
			}()
			server.RunHTTPServer(sunshineAddr, port, isLog)
			return nil
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 24631, "port on which the sunshine service listens")
	cmd.Flags().StringVarP(&sunshineAddr, "addr", "a", "", "address of the front-end page requesting the sunshine service, e.g. http://192.168.1.10:24631 or https://your-domain.com")
	cmd.Flags().BoolVarP(&isLog, "log", "l", false, "enable service logging")
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
