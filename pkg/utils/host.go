// Package utils is a library of commonly used utility functions.
package utils

import (
	"fmt"
	"net"
	"os"
)

// GetLocalIP 获取本机非回环IPv4地址，用于服务注册时确保实例ID唯一。
// 如果获取失败，返回 "unknown"。
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "unknown"
}

// GetHostname get hostname
func GetHostname() string {
	name, err := os.Hostname()
	if err != nil {
		name = "unknown"
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
