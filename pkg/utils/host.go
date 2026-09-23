// Package utils is a library of commonly used utility functions.
package utils

import (
	"fmt"
	"net"
	"os"
)

const unknownIP = "unknown"

// GetLocalIP 获取本机非回环IPv4地址，用于服务注册时确保实例ID唯一。
// 如果获取失败，返回 "unknown"。
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return unknownIP
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return unknownIP
}

// ResolveHost 按优先级解析服务注册地址，用于替代配置文件中的硬编码 host。
// 优先级: 环境变量 POD_IP > 环境变量 HOST_IP > 自动检测本机网卡 IP > 兜底 127.0.0.1。
// 适用于 K8s (Downward API 注入 POD_IP)、Docker、物理机、本地开发等场景。
func ResolveHost() string {
	// 1. K8s Pod IP（Downward API 注入）
	if ip := os.Getenv("POD_IP"); ip != "" {
		return ip
	}
	// 2. 物理机 / VM IP（Docker 或自定义注入）
	if ip := os.Getenv("HOST_IP"); ip != "" {
		return ip
	}
	// 3. 自动检测本机非回环网卡 IP
	if ip := GetLocalIP(); ip != unknownIP {
		return ip
	}
	// 4. 兜底（仅本地开发）
	return "127.0.0.1"
}

// GetHostname get hostname
func GetHostname() string {
	name, err := os.Hostname()
	if err != nil {
		name = unknownIP
	}
	return name
}

// GetLocalHTTPAddrPairs get available http server and request address
func GetLocalHTTPAddrPairs() (serverAddr string, requestAddr string) {
	port, err := GetAvailablePort()
	if err != nil {
		fmt.Printf("GetAvailablePort error: %v\n", err)
		return "", ""
	}
	serverAddr = fmt.Sprintf(":%d", port)
	requestAddr = fmt.Sprintf("http://127.0.0.1:%d", port)
	return serverAddr, requestAddr
}

// GetAvailablePort get available port
func GetAvailablePort() (int, error) {
	address, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:0", "0.0.0.0"))
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			fmt.Printf("close listener error: %v\n", closeErr)
		}
	}()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to cast listener address to TCPAddr")
	}
	port := tcpAddr.Port

	return port, nil
}
