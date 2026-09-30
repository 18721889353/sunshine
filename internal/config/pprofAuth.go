package config

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// PprofIPWhitelist 返回 HTTP 鉴权中间件，仅允许指定 IP/CIDR 白名单内的请求访问 pprof。
//
// 支持两种格式：纯 IP（如 127.0.0.1）和 CIDR（如 10.0.0.0/8）。
// 如果传入的列表为空，使用默认内网段（10.0.0.0/8、172.16.0.0/12、192.168.0.0/16）。
//
// 该函数在启动注册和 Nacos 配置热更新时都会被调用，
// 每次调用返回一个携带最新白名单的中间件闭包。
//
// 参数:
//   - cidrs: IP/CIDR 白名单列表。
//
// 返回值:
//   - func(http.Handler) http.Handler: HTTP 中间件处理函数。
func PprofIPWhitelist(cidrs []string) func(http.Handler) http.Handler {
	if len(cidrs) == 0 {
		cidrs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	ipNets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			ipNets = append(ipNets, ipNet)
		} else if ip := net.ParseIP(cidr); ip != nil {
			mask := net.CIDRMask(32, 32)
			if ip.To4() == nil {
				mask = net.CIDRMask(128, 128)
			}
			ipNets = append(ipNets, &net.IPNet{IP: ip.Mask(mask), Mask: mask})
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 优先从 X-Real-IP 和 X-Forwarded-For 获取真实客户端 IP
			// 兼容网关代理场景（Nginx、Kong、API Gateway 等）
			realIP := r.Header.Get("X-Real-IP")
			if realIP == "" {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					if i := strings.IndexByte(xff, ','); i > 0 {
						realIP = strings.TrimSpace(xff[:i])
					} else {
						realIP = strings.TrimSpace(xff)
					}
				}
			}
			if realIP == "" {
				realIP = r.RemoteAddr
				if host, _, err := net.SplitHostPort(realIP); err == nil {
					realIP = host
				}
			}
			for _, ipNet := range ipNets {
				if ipNet.Contains(net.ParseIP(realIP)) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, fmt.Sprintf("Forbidden: IP %s 不在白名单中", realIP), http.StatusForbidden)
		})
	}
}

// PprofIPWhitelistGin 返回 Gin 鉴权中间件，功能与 PprofIPWhitelist 等价，
// 供 gin 版 pprof 路由（pkg/gin/prof）启动注册与 Nacos 热更新使用。
//
// 支持两种格式：纯 IP（如 127.0.0.1）和 CIDR（如 10.0.0.0/8）。
// 如果传入的列表为空，使用默认内网段（含 IPv6 本地回环）。
//
// 该函数在启动注册（routers.go）和配置热更新时都会被调用，
// 热更新经由 ginprof.SetPprofAuth 热替换，每次调用返回携带最新白名单的中间件闭包。
//
// 参数:
//   - cidrs: IP/CIDR 白名单列表。
//
// 返回值:
//   - gin.HandlerFunc: Gin 中间件处理函数。
func PprofIPWhitelistGin(cidrs []string) gin.HandlerFunc {
	if len(cidrs) == 0 {
		cidrs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128"}
	}
	ipNets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			ipNets = append(ipNets, ipNet)
		} else if ip := net.ParseIP(cidr); ip != nil {
			// 纯 IP 自动转为 /32（IPv4）或 /128（IPv6）
			mask := net.CIDRMask(32, 32)
			if ip.To4() == nil {
				mask = net.CIDRMask(128, 128)
			}
			ipNets = append(ipNets, &net.IPNet{IP: ip.Mask(mask), Mask: mask})
		}
	}
	return func(c *gin.Context) {
		realIP := c.ClientIP()
		for _, ipNet := range ipNets {
			if ipNet.Contains(net.ParseIP(realIP)) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Forbidden: IP %s 不在白名单中", realIP)})
	}
}
