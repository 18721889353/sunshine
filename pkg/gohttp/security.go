package gohttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// ========== SSRF 防护 ==========

// isPrivateIP 检查是否为私有/内网 IP（回环、链路本地、私有地址段、运营商 NAT、云元数据）
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 私有地址段
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"169.254.0.0/16",
	}
	for _, block := range privateBlocks {
		_, cidr, err := net.ParseCIDR(block)
		if err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfLookup 域名解析入口，抽为包级变量以便单测注入解析结果（无网络依赖，
// 见 TestApplySSRFProtectionBlocksDomain）；生产环境始终使用 net.DefaultResolver
var ssrfLookup = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// applySSRFProtection 在 Transport 上装配 SSRF 双层防护。
//
// 第一层（IP 字面量）：拨号地址本身就是 IP 时直接校验。
// 第二层（域名）：先用 net.DefaultResolver 完成解析，逐个校验解析结果，
// 全部为公网地址后**对已校验的 IP 直接拨号**（而不是回拨域名），从而关闭
// 「校验完成到再次解析之间」的 DNS 重绑定（rebinding）窗口。
//
// 历史缺陷：原实现只在 host 能被 net.ParseIP 解析时才拦截，域名指向内网
// （如攻击者控制的 DNS 记录指向 127.0.0.1）可完全绕过防护；本函数与
// TestApplySSRFProtectionBlocksDomain 一起防回归。
func applySSRFProtection(t *http.Transport) {
	originalDial := t.DialContext
	if originalDial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		originalDial = d.DialContext
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}

		// 第一层：IP 字面量直接校验
		if ip := net.ParseIP(host); ip != nil {
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("gohttp: 目标地址 %s 属于内网: %w", addr, ErrSSRFBlocked)
			}
			return originalDial(ctx, network, addr)
		}

		// 第二层：域名解析后校验全部结果，再对首个已校验 IP 拨号
		ips, err := ssrfLookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("gohttp: 解析域名 %q 失败: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("gohttp: 域名 %q 未解析到任何地址", host)
		}
		for _, ipa := range ips {
			if isPrivateIP(ipa.IP) {
				return nil, fmt.Errorf("gohttp: 域名 %s 解析到内网地址 %s: %w", host, ipa.IP, ErrSSRFBlocked)
			}
		}
		return originalDial(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// ValidateURL 校验URL合法性（协议白名单 + 主机名校验 + IP 字面量内网拦截）。
//
// 注意职责边界：本函数只做**语法与字面量**校验，域名指向内网的攻击需要
// WithSSRFProtection 在拨号层拦截，两者互补，不能互相替代。
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("gohttp: URL 不能为空")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("gohttp: URL 格式非法: %w", err)
	}

	// 只允许 http 和 https 协议
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("gohttp: 仅允许 http/https 协议，当前为 %q", u.Scheme)
	}

	// 检查主机部分
	if u.Hostname() == "" {
		return errors.New("gohttp: URL 主机名不能为空")
	}

	// 如果主机是IP，检查是否为内网
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		if isPrivateIP(ip) {
			return ErrSSRFBlocked
		}
	}

	return nil
}

// ========== 熔断器（连续失败计数 + 冷却 + 半开探测） ==========

// defaultCircuitBreakerCooldown 打开状态的冷却时长。
// 取值依据：工业界惯例区间 5~30s（Hystrix 默认 5s，Envoy 默认 30s），此处取中间值 10s，
// 属经验余量而非本包实测读数；单测通过 newCircuitBreaker 注入毫秒级冷却，不依赖该值。
const defaultCircuitBreakerCooldown = 10 * time.Second

// breakerState 熔断器状态
type breakerState int

const (
	breakerClosed   breakerState = iota // 关闭：正常放行
	breakerOpen                         // 打开：拒绝所有请求
	breakerHalfOpen                     // 半开：冷却结束后放行单个探测请求
)

// circuitBreaker 简易熔断器：threshold 次连续失败后打开，冷却期满放行一个探测，
// 探测成功回到关闭状态，失败则重新打开。并发安全（内部互斥锁）。
type circuitBreaker struct {
	mu        sync.Mutex
	threshold int           // 连续失败阈值
	cooldown  time.Duration // 打开状态冷却时长
	failures  int           // 当前连续失败次数（仅关闭状态累计）
	state     breakerState
	openedAt  time.Time // 进入打开状态的时间
	probing   bool      // 半开状态下是否已有探测请求在途
}

// newCircuitBreaker 创建熔断器（cooldown 由测试注入短值，生产用 defaultCircuitBreakerCooldown）
func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{threshold: threshold, cooldown: cooldown}
}

// allow 判断当前是否放行请求：返回 nil 放行；返回错误（包裹 ErrCircuitBreakerOpen）表示拒绝。
// 打开状态冷却期满时自动转入半开并放行唯一探测请求。
// nil receiver 安全：未启用熔断时直接放行。
func (b *circuitBreaker) allow() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerClosed:
		return nil
	case breakerOpen:
		if time.Since(b.openedAt) >= b.cooldown {
			b.state = breakerHalfOpen
			b.probing = true
			return nil
		}
		return fmt.Errorf("gohttp: 熔断器已打开（连续失败 %d 次），冷却 %s 后重试: %w",
			b.threshold, b.cooldown, ErrCircuitBreakerOpen)
	default: // breakerHalfOpen
		if b.probing {
			return fmt.Errorf("gohttp: 熔断器半开状态已有探测请求在途: %w", ErrCircuitBreakerOpen)
		}
		b.probing = true
		return nil
	}
}

// onSuccess 记录一次成功：半开探测成功则回到关闭状态，关闭状态清零连续失败计数。
func (b *circuitBreaker) onSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerHalfOpen:
		b.state = breakerClosed
		b.failures = 0
		b.probing = false
	case breakerClosed:
		b.failures = 0
	}
	// 打开状态不会收到成功（请求已在 allow 处被拒绝）
}

// onFailure 记录一次失败：关闭状态下累计至阈值则打开；半开探测失败则重新打开并重置冷却计时。
func (b *circuitBreaker) onFailure() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerClosed:
		b.failures++
		if b.failures >= b.threshold {
			b.state = breakerOpen
			b.openedAt = time.Now()
		}
	case breakerHalfOpen:
		b.state = breakerOpen
		b.openedAt = time.Now()
		b.probing = false
	}
}

// ========== 错误脱敏 ==========

// redactedError 对外呈现脱敏后的错误文本，同时保留完整错误链（errors.Is/As 可穿透）。
// 与 pkg/goredis 的 redactedDSNError 同构。
type redactedError struct {
	err error
	msg string
}

// Error 返回脱敏后的消息（不含 URL 中的密码与敏感查询参数）
func (e *redactedError) Error() string { return e.msg }

// Unwrap 返回原始错误，保持错误链完整
func (e *redactedError) Unwrap() error { return e.err }

// 敏感查询参数名单（命中即把值替换为 ***）。刻意不收录歧义大的 key：它常用于
// 分页/排序等业务字段，而 mykey 之类参数名的误匹配也应避免。
var sensitiveQueryPattern = regexp.MustCompile(
	`(?i)((?:[?&])(?:access_token|refresh_token|id_token|token|api_key|apikey|client_secret|secret|password|passwd|pwd|authorization|signature)=)[^&\s]*`)

// userinfoPasswordPattern 匹配 URL 中的 user:password@ 片段
var userinfoPasswordPattern = regexp.MustCompile(`(://[^/\s:@]+:)[^@\s]+(@)`)

// redactURL 对错误文本中的 URL 凭据与敏感查询参数脱敏。
// 覆盖两类泄漏面：userinfo 密码（redis://user:pass@host 形态）与敏感 query 参数
// （?token=xxx 形态）。纯函数、无副作用，可安全用于日志与 Span 属性。
func redactURL(s string) string {
	s = userinfoPasswordPattern.ReplaceAllString(s, `${1}***${2}`)
	s = sensitiveQueryPattern.ReplaceAllString(s, `${1}***`)
	return s
}
