# integration_test.go 骨架

把 `<SVC>` / `<svc>` / `<name>` 替换为实际服务名（如 `NACOS` / `nacos` / `nacoscli`）。
参考实现：`pkg/nacoscli/integration_test.go`。

## 一、文件头：build tag + TestMain + loadDotEnv

```go
//go:build integration

package <name>

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// defaultIntegrationTimeout 集成测试总时长的兜底上限，防止外部 SDK 的 goroutine 泄漏导致 go test 永远挂起。
const defaultIntegrationTimeout = 120 * time.Second

// integrationTimeout 返回总时长上限，由 <SVC>_TEST_TIMEOUT 配置（Go duration，如 "180s"、"5m"）：
// 用例增多或网络较慢时上调即可，无需改代码。未设置或取值非法时使用兜底值。
func integrationTimeout() time.Duration {
	v := os.Getenv("<SVC>_TEST_TIMEOUT")
	if v == "" {
		return defaultIntegrationTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "WARN: <SVC>_TEST_TIMEOUT=%q 不是有效的正 duration，使用兜底值 %s\n", v, defaultIntegrationTimeout)
		return defaultIntegrationTimeout
	}
	return d
}

// loadDotEnv 在包目录存在 .env 时加载测试所需环境变量，仅填充尚未设置的项（显式 export 优先）。
//
// 为何需要它：README 写的「使用包内 .env」在 Go/go test 里**不会自动生效**，
// 实际依赖 IDE 的 EnvFile 插件或手动 export；而含裸括号/等号的值（如 `ENC(...)`）在 bash 下
// `source .env` 会直接语法报错。
//
// 解析规则（不引入第三方 dotenv 依赖）：
//   - 跳过空行与 `#` 注释行；按**第一个** `=` 分割 key/value（值可含 `=` 与括号）
//   - 去除成对的引号，其余内容原样保留（不做变量展开）
//   - **已存在且非空的环境变量不覆盖**，便于 CI 用真实变量覆盖本地 .env
//
// 仅在 `-tags=integration` 的测试进程中生效，不影响生产代码；.env 不存在时静默返回 0。
func loadDotEnv(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var loaded int
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return loaded, err
		}
		loaded++
	}

	return loaded, nil
}

// TestMain 先补 .env，再强制限制总时长。
func TestMain(m *testing.M) {
	if n, err := loadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: 读取 .env 失败，仅使用进程环境变量: %v\n", err)
	} else if n > 0 {
		fmt.Printf("已从 .env 加载 %d 个环境变量（不覆盖已设置的变量）\n", n)
	}

	timeout := integrationTimeout()
	ch := make(chan int, 1)
	go func() {
		ch <- m.Run()
	}()

	select {
	case code := <-ch:
		os.Exit(code)
	case <-time.After(timeout):
		fmt.Fprintf(os.Stderr,
			"FATAL: 集成测试超过 %s 超时（可能是外部 SDK goroutine 泄漏，可用 <SVC>_TEST_TIMEOUT 上调）\n", timeout)
		os.Exit(1)
	}
}
```

## 二、require* 前置检查族

```go
// require<Svc> 从环境变量读取连接信息，未设置时跳过。
func requireSvc(t *testing.T) (host string, port int, namespaceID, group, dataID string) {
	t.Helper()
	host = os.Getenv("<SVC>_IP_ADDR")
	portStr := os.Getenv("<SVC>_PORT")
	if host == "" || portStr == "" {
		t.Skip("<SVC>_IP_ADDR 或 <SVC>_PORT 未设置，跳过集成测试")
	}
	var err error
	port, err = strconv.Atoi(portStr)
	require.NoError(t, err, "<SVC>_PORT 解析失败")
	// 可选变量在此补默认值
	return host, port, namespaceID, group, dataID
}

// requireAuth 读取认证信息，未设置时跳过认证类测试。
func requireAuth(t *testing.T) (username, password string) {
	t.Helper()
	username, password = os.Getenv("<SVC>_USERNAME"), os.Getenv("<SVC>_PASSWORD")
	if username == "" || password == "" {
		t.Skip("<SVC>_USERNAME 或 <SVC>_PASSWORD 未设置，跳过认证测试")
	}
	return username, password
}

// dialErr 探测端口能否建立 TCP 连接（3 秒超时），可达时返回 nil。
// 返回 error 而不是 bool：跳过日志需带上 `connection refused`/`i/o timeout` 等具体原因，
// 才能区分「端口没映射」「IP 写错」「防火墙丢包」三类环境问题。
func dialErr(host string, port int) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// requirePortReachable 统一可达性前置检查，未配置或不可达时跳过并返回端口。
// reason 用于在跳过日志里区分是哪类用例。
func requirePortReachable(t *testing.T, host, reason string) int {
	t.Helper()
	port := requirePort(t)
	if err := dialErr(host, port); err != nil {
		t.Skipf("端口 %d 不可达（%s），跳过: %v", port, reason, err)
	}
	return port
}
```

### 错误归类函数（Skip vs Fail 的判据）

```go
// isAuthErr 判断错误是否为「服务端鉴权拒绝」。部分用例故意不传凭据（验证无认证路径），
// 而服务端开启鉴权时匿名请求会被拒（实测 `Code: 401, Message: User not found!`），
// 这是部署选择而非代码缺陷，应跳过而不是失败。
func isAuthErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "user not found") ||
		strings.Contains(msg, "unknown user") ||
		strings.Contains(msg, "code: 401") ||
		strings.Contains(msg, "forbidden")
}

// isExpectedBizErr 判断是否「预期内的业务失败」（如配置已加密但无密钥）。
//
// 陷阱（实测得出，勿随意加关键词）：**不要**把 `config encrypted data key` 加进来。
// SDK 读不到本地缓存文件时会把该字样拼进错误文本，它表达的是「缓存文件不存在」而非「配置已加密」，
// 加进来会把基础设施故障误判为预期失败并静默放过。
func isExpectedBizErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cipher") ||
		strings.Contains(msg, "decrypt") ||
		strings.Contains(msg, "enc(")
}
```

## 三、等待常量（每个都写三要素注释）

```go
// 监听/注册类用例的预热与等待时长。
//
// 为何只能固定预热：外部 SDK 的注册 API 是非阻塞的，且未暴露「注册已生效」的可观测信号。
// 为何不会因而 flaky：结果判定一律靠下面的「轮询 + deadline」循环，预热不足只会让回调晚到，不会造成假失败。
// **已在真实环境验证**（<环境>，<N> 轮全绿）：<实测读数>。
const (
	listenRegistrationWarmup = 3 * time.Second  // <依据：实测读数 or 经验余量，必须点明是哪种>
	watchRegistrationWarmup  = 2 * time.Second
	retryCompletionWait      = 5 * time.Second
	readyWait                = 10 * time.Second
	externalChangeDeadline   = 20 * time.Second // 实测亚秒~2s 级，取 20s 留数量级余量
	externalIdleWait         = 15 * time.Second // 空闲窗口，用于覆盖「长连接空闲后失效」；15s 是经验值，非服务端某个周期
)
```

## 四、轮询到「可见」而不是单次读数下结论

```go
// pollReady 在 timeout 内轮询 probe，直到拿到 want 个结果或超时。
//
// 注意外部 SDK 的坑：列表为空时它**直接返回 error**（如 `instance list is empty!`）而不是空切片，
// 因此 error 也要当作「尚未可见」继续重试，只有超时后才把最后一次的 error 交给调用方判定。
//
// 实测现象：同一用例在相邻两轮里一轮立即查到、一轮报 empty —— 说明新建资源对查询方并非立即可见。
func pollReady(probe func() (int, error), want int, timeout time.Duration) (got int, attempts int, lastErr error) {
	deadline := time.Now().Add(timeout)
	for {
		attempts++
		got, lastErr = probe()
		if got >= want {
			return got, attempts, nil
		}
		if time.Now().After(deadline) {
			return got, attempts, lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}
```

## 五、外部视角探针（证明「别人改了也能感知」）

```go
// publishViaOpenAPI 通过服务自身的 HTTP Open API 修改配置，等价于「在控制台点发布」。
//
// 为什么需要它：若修改方也用 SDK（走 gRPC、且与监听方共用 SDK 客户端缓存），
// 只能证明「SDK 发布 → SDK 监听」这一自闭环；而 Open API 才是控制台、运维脚本、CI 实际使用的入口。
//
// 返回 error 时调用方应 Skipf 而非 Fatalf：服务端可能禁用该 API 或鉴权方式不同，属环境差异而非代码缺陷。
func publishViaOpenAPI(t *testing.T, host string, port int, namespaceID, group, dataID, content, username, password string) error {
	t.Helper()
	base := fmt.Sprintf("http://%s:%d%s", host, port, contextPath())
	reqCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	query := url.Values{}
	if token, loginErr := accessToken(reqCtx, base, username, password); loginErr != nil {
		t.Logf("Open API 登录未成功，按匿名请求继续: %v", loginErr) // 未开鉴权时登录接口非 200，不算错误
	} else if token != "" {
		query.Set("accessToken", token)
	}

	form := url.Values{"dataId": {dataID}, "group": {group}, "content": {content}}
	if namespaceID != "" {
		form.Set("tenant", namespaceID) // 注意：v1 API 用 tenant 表达命名空间，与 SDK 的 NamespaceId 同义
	}
	// POST base+"/v1/cs/configs?"+query.Encode()，Content-Type: application/x-www-form-urlencoded
	// 成功判据：HTTP 200 **且** 响应体 TrimSpace 后等于 "true"
	return nil
}

// awaitChange 按**内容**等待目标变更送达。
//
// 为什么不能「收到任意一次回调就算通过」：注册动作之前发生的变更可能被补推一次，会造成假阳性。
func awaitChange(t *testing.T, arrivals <-chan string, want string, publishedAt time.Time, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()

	var others int
	for {
		select {
		case got := <-arrivals:
			if got == want {
				t.Logf("目标变更已送达: 延迟 %s（此前收到其它回调 %d 次）", time.Since(publishedAt), others)
				return
			}
			others++
		case <-timer.C:
			t.Fatalf("%s 内未收到目标变更回调（等待期间收到其它回调 %d 次，均非目标内容）", deadline, others)
			return
		}
	}
}
```

### 三轮外部变更用例（活性验证的标准形）

```go
func TestIntegration_WatchExternalChange(t *testing.T) {
	host, port, namespaceID, group := requireSvc(t)
	username, password := requireAuth(t)
	endpoint := requirePortReachable(t, host, "外部变更检测")

	dataID := testKey()
	// 1. 用 SDK 只准备初始数据（不用于触发变更）
	// 2. 注册监听（全程只读），预热固定窗口
	// 3. 第 1、2 次：外部 Open API 修改 + awaitChange（按内容匹配）
	// 4. time.Sleep(externalIdleWait) 后第 3 次修改 —— 覆盖「长连接空闲后失效」
	// 5. 断言：至少收到 3 次回调；并断言没有多余回调
}
```

## 六、断言降级时的记录形（不可控读数不要硬断言）

```go
if len(got) < want {
	// 读数不全时补齐两份视图，便于定位卡在哪一层（均为实测读数，不做无依据归因）：
	// 客户端缓存数（不过滤状态）+ 服务端 Open API 快照。
	cacheCount := countFromClientCache(...)
	serverRows, serverErr := listViaOpenAPI(...)
	t.Logf("未达 %d 个 -> 客户端=%d(轮询%d次), 缓存=%d, 服务端=%v (err=%v)",
		want, len(got), attempts, cacheCount, serverRows, serverErr)
}
// 断言只覆盖「可稳定复现的事实」（如 ≥1），把不可控的「看到几个」降级为日志。
// 必须在注释写清：实测证据 + 归属判断（部署层面 or 代码缺陷）。
```

## 七、常用小工具

```go
// testKey 生成唯一的临时资源 ID，测试后清理。
func testKey() string { return fmt.Sprintf("<name>-test-%d", time.Now().UnixNano()) }

// httpBodySnippet 截取响应体前 200 字节，避免把整页 HTML 错误打进测试日志。
func httpBodySnippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		return s[:200] + "...(截断)"
	}
	return s
}

// registerWithRetry 重试到成功或超时：外部 SDK 的首次调用常撞上连接未就绪（返回 false / error）。
func registerWithRetry(do func() (bool, error), timeout time.Duration) (bool, error)
```

## 八、包内 `.env` 模板

```ini
# 集成测试连接信息（仅本地使用；根 .gitignore 的 *.env 已覆盖本文件，勿提交真实凭据）
<SVC>_IP_ADDR=
<SVC>_PORT=
<SVC>_NAMESPACE_ID=
<SVC>_GROUP=dev
<SVC>_DATA_ID=
<SVC>_USERNAME=
<SVC>_PASSWORD=
<SVC>_GRPC_PORT=
<SVC>_CONTEXT_PATH=/nacos
<SVC>_TEST_TIMEOUT=300s
```
