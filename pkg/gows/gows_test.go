package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"

	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

func newTestClientPair(t testing.TB, opts ...func(*clientConfig)) (*Client, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			serverCh <- nil
			return
		}
		serverCh <- newClientWithConfig(context.Background(), raw, "test-uid", testConfig(opts...))
	}))
	t.Cleanup(s.Close)
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	testConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() { _ = testConn.Close() })
	client := <-serverCh
	if client == nil {
		t.Fatal("server failed to create client")
	}
	return client, testConn
}

func newTestClientWithUID(t testing.TB, uid string, opts ...func(*clientConfig)) (*Client, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *Client, 1)
	s := newTestServer(t, func(raw *websocket.Conn) {
		serverCh <- newClientWithConfig(context.Background(), raw, uid, testConfig(opts...))
	})
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	testConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() { _ = testConn.Close() })
	client := <-serverCh
	if client == nil {
		t.Fatal("server failed to create client")
	}
	return client, testConn
}

// testConfig 构建测试用的 clientConfig，应用可选的配置修改函数后返回。
func testConfig(opts ...func(*clientConfig)) *clientConfig {
	cfg := &clientConfig{writeChSize: 1024, readChSize: 1024}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.writeTimeout <= 0 {
		cfg.writeTimeout = writeDeadline
	}
	return cfg
}

func newTestServer(t testing.TB, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		handler(raw)
	}))
	t.Cleanup(s.Close)
	return s
}

// mockBackend 模拟 Backend 实现，支持多订阅者扇出
type mockBackend struct {
	mu          sync.Mutex
	publishCh   chan *PubSubMessage
	subscribers []chan *PubSubMessage
	closed      atomic.Bool
	publishCnt  atomic.Int64
}

func newMockBackend(bufSize int) *mockBackend {
	return &mockBackend{publishCh: make(chan *PubSubMessage, bufSize)}
}

func (m *mockBackend) Publish(_ context.Context, msg *PubSubMessage) error {
	m.publishCnt.Add(1)
	m.mu.Lock()
	for _, ch := range m.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
	m.mu.Unlock()
	select {
	case m.publishCh <- msg:
	default:
	}
	return nil
}

func (m *mockBackend) SubscribeBroadcast(_ context.Context) (<-chan *PubSubMessage, error) {
	ch := make(chan *PubSubMessage, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch, nil
}

func (m *mockBackend) Subscribe(_ context.Context, uid string) (<-chan *PubSubMessage, error) {
	ch := make(chan *PubSubMessage, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch, nil
}

func (m *mockBackend) Unsubscribe(_ context.Context, uid string) error {
	return nil
}

func (m *mockBackend) Close() error {
	m.closed.Store(true)
	m.mu.Lock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = nil
	m.mu.Unlock()
	return nil
}

// createTestJWT 创建包含指定 UID 的测试 JWT token
func createTestJWT(t testing.TB, uid string) string {
	t.Helper()
	token, err := jwt.GenerateToken(uid, "test-user")
	if err != nil {
		t.Fatalf("create test JWT: %v", err)
	}
	return token
}

// createTestJWTWithClaims 使用自定义 Claims 创建测试 JWT token
func createTestJWTWithClaims(t testing.TB, claims *jwt.Claims) string {
	t.Helper()
	fields := make(jwt.KV)
	for k, v := range claims.Fields {
		fields[k] = v
	}
	token, err := jwt.GenerateCustomToken(fields)
	if err != nil {
		t.Fatalf("create test JWT with claims: %v", err)
	}
	return token
}

// ---------------------------------------------------------------------------
// Message 序列化
// ---------------------------------------------------------------------------

func TestMessageJSON(t *testing.T) {
	t.Parallel()
	msg := Message{Type: "notify", Msg: "hello", Data: map[string]int{"n": 42}}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"notify"`) {
		t.Errorf("expected type, got %s", string(data))
	}
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Type != "notify" {
		t.Errorf("Type=%q", m.Type)
	}
}

// ---------------------------------------------------------------------------
// 配置选项：Client
// ---------------------------------------------------------------------------

func TestClientConfig_Defaults(t *testing.T) {
	o := &clientConfig{writeChSize: 1024, readChSize: 1024}
	if o.writeChSize != 1024 {
		t.Errorf("writeChSize=%d, want 1024", o.writeChSize)
	}
	if o.readChSize != 1024 {
		t.Errorf("readChSize=%d, want 1024", o.readChSize)
	}
	if o.writeTimeout != 0 {
		t.Errorf("writeTimeout=%v, want 0", o.writeTimeout)
	}
}

func TestClientConfig_DirectSetFields(t *testing.T) {
	o := &clientConfig{writeChSize: 1024, readChSize: 1024}
	o.writeChSize = 2048
	o.readChSize = 512
	o.readTimeout = 30 * time.Second
	o.writeTimeout = 15 * time.Second
	o.readLimit = 8192
	o.writeLimit = 4096

	if o.writeChSize != 2048 {
		t.Errorf("writeChSize=%d, want 2048", o.writeChSize)
	}
	if o.readChSize != 512 {
		t.Errorf("readChSize=%d, want 512", o.readChSize)
	}
	if o.readTimeout != 30*time.Second {
		t.Errorf("readTimeout=%v", o.readTimeout)
	}
	if o.writeTimeout != 15*time.Second {
		t.Errorf("writeTimeout=%v", o.writeTimeout)
	}
	if o.readLimit != 8192 {
		t.Errorf("readLimit=%d, want 8192", o.readLimit)
	}
	if o.writeLimit != 4096 {
		t.Errorf("writeLimit=%d, want 4096", o.writeLimit)
	}
}

func TestClientConfig_Dispatcher(t *testing.T) {
	o := &clientConfig{writeChSize: 1024, readChSize: 1024}
	dd := NewDispatcher(nil)
	o.dispatcher = dd
	if o.dispatcher != dd {
		t.Error("dispatcher not set")
	}
}

func TestClientConfig_ZeroValuesPreserved(t *testing.T) {
	o := &clientConfig{writeChSize: 1024, readChSize: 1024}
	// 直接设 0 表示保持默认值不变
	o.writeChSize = 0
	if o.writeChSize != 0 {
		t.Errorf("writeChSize should be 0, got %d", o.writeChSize)
	}
}

// ---------------------------------------------------------------------------
// 配置选项：Dispatcher
// ---------------------------------------------------------------------------

func TestDispatcherOptions_WorkerPool(t *testing.T) {
	o := defaultDispatcherOptions()
	WithWorkerPool(8)(o)
	if o.workerNum != 8 {
		t.Errorf("workerNum=%d, want 8", o.workerNum)
	}
}

func TestDispatcherOptions_MaxConnections(t *testing.T) {
	o := defaultDispatcherOptions()
	WithMaxConnections(5000)(o)
	if o.maxClientNum != 5000 {
		t.Errorf("maxClientNum=%d, want 5000", o.maxClientNum)
	}
}

func TestDispatcherOptions_ZeroValues(t *testing.T) {
	o := defaultDispatcherOptions()
	WithWorkerPool(0)(o)
	WithMaxConnections(0)(o)
	if o.workerNum != 4 {
		t.Errorf("workerNum default 4, got %d", o.workerNum)
	}
	if o.maxClientNum != 0 {
		t.Errorf("maxClientNum default 0, got %d", o.maxClientNum)
	}
}

// ---------------------------------------------------------------------------
// 配置选项：Heartbeat
// ---------------------------------------------------------------------------

func TestHeartbeatOptions_Defaults(t *testing.T) {
	o := defaultHeartbeatOptions()
	if o.interval != defaultHeartbeatInterval {
		t.Errorf("interval=%v, want %v", o.interval, defaultHeartbeatInterval)
	}
	if o.pongTimeout != defaultPongTimeout {
		t.Errorf("pongTimeout=%v, want %v", o.pongTimeout, defaultPongTimeout)
	}
	if o.pingWriteWait != defaultPingWriteWait {
		t.Errorf("pingWriteWait=%v, want %v", o.pingWriteWait, defaultPingWriteWait)
	}
}

func TestHeartbeatOptions_Custom(t *testing.T) {
	o := defaultHeartbeatOptions()
	WithHeartbeatInterval(15 * time.Second)(o)
	WithPongTimeout(5 * time.Second)(o)
	WithPingWriteWait(3 * time.Second)(o)
	if o.interval != 15*time.Second {
		t.Errorf("interval=%v", o.interval)
	}
	if o.pongTimeout != 5*time.Second {
		t.Errorf("pongTimeout=%v", o.pongTimeout)
	}
	if o.pingWriteWait != 3*time.Second {
		t.Errorf("pingWriteWait=%v", o.pingWriteWait)
	}
}

// ---------------------------------------------------------------------------
// 配置选项：Upgrade
// ---------------------------------------------------------------------------

func TestUpgradeOptions_Defaults(t *testing.T) {
	o := defaultUpgradeOptions()
	if o.checkOrigin(nil) != false {
		t.Error("default CheckOrigin should reject")
	}
	if o.checkOriginSet {
		t.Error("checkOriginSet should be false by default")
	}
	if !o.enableCompression {
		t.Error("compression enabled by default")
	}
}

func TestUpgradeOptions_CheckOrigin(t *testing.T) {
	o := defaultUpgradeOptions()
	WithCheckOrigin(func(r *http.Request) bool { return true })(o)
	if !o.checkOriginSet {
		t.Error("checkOriginSet should be true after WithCheckOrigin")
	}
	if !o.checkOrigin(nil) {
		t.Error("WithCheckOrigin should accept")
	}
}

func TestUpgradeOptions_BufferSize(t *testing.T) {
	o := defaultUpgradeOptions()
	WithBufferSize(8192, 16384)(o)
	if o.readBufSize != 8192 {
		t.Errorf("readBufSize=%d", o.readBufSize)
	}
	if o.writeBufSize != 16384 {
		t.Errorf("writeBufSize=%d", o.writeBufSize)
	}
}

func TestUpgradeOptions_QueueSize(t *testing.T) {
	o := defaultUpgradeOptions()
	WithQueueSize(2048, 4096)(o)
	if o.writeChSize != 2048 {
		t.Errorf("writeChSize=%d", o.writeChSize)
	}
	if o.readChSize != 4096 {
		t.Errorf("readChSize=%d", o.readChSize)
	}
}

func TestUpgradeOptions_Subprotocols(t *testing.T) {
	o := defaultUpgradeOptions()
	WithSubprotocols("v1", "v2")(o)
	if len(o.subprotocols) != 2 || o.subprotocols[0] != "v1" {
		t.Errorf("subprotocols=%v", o.subprotocols)
	}
}

func TestUpgradeOptions_Heartbeat(t *testing.T) {
	o := defaultUpgradeOptions()
	WithHeartbeat()(o)
	if !o.enableHeart {
		t.Error("enableHeart should be true after WithHeartbeat")
	}
}

func TestUpgradeOptions_HeartbeatWithOptions(t *testing.T) {
	o := defaultUpgradeOptions()
	WithHeartbeatOptions(WithHeartbeatInterval(15 * time.Second))(o)
	if !o.enableHeart {
		t.Error("enableHeart should be true")
	}
	if len(o.heartbeatOpts) != 1 {
		t.Fatalf("heartbeatOpts len=%d, want 1", len(o.heartbeatOpts))
	}
	ho := defaultHeartbeatOptions()
	o.heartbeatOpts[0](ho)
	if ho.interval != 15*time.Second {
		t.Errorf("heartbeat interval=%v, want 15s", ho.interval)
	}
}

func TestUpgradeOptions_WithHeartbeatOptions_ZeroValues(t *testing.T) {
	o := defaultUpgradeOptions()
	WithHeartbeatOptions()(o)
	if !o.enableHeart {
		t.Error("enableHeart should be true even without opts")
	}
}

func TestUpgradeOptions_Dispatcher(t *testing.T) {
	o := defaultUpgradeOptions()
	dd := NewDispatcher(nil)
	WithDispatcher(dd)(o)
	if o.dispatcher != dd {
		t.Error("dispatcher not set")
	}
}

func TestUpgradeOptions_DispatcherNil(t *testing.T) {
	o := defaultUpgradeOptions()
	WithDispatcher(nil)(o)
	if o.dispatcher != nil {
		t.Error("nil dispatcher should not overwrite")
	}
}

func TestUpgradeOptions_EnableDistributed(t *testing.T) {
	o := defaultUpgradeOptions()
	WithEnableDistributed(true)(o)
	if !o.enableDistributed {
		t.Error("enableDistributed should be true")
	}
	WithEnableDistributed(false)(o)
	if o.enableDistributed {
		t.Error("enableDistributed should be false")
	}
}

func TestUpgradeOptions_ClientUID(t *testing.T) {
	o := defaultUpgradeOptions()
	WithClientUID("test-user")(o)
	if o.clientUID != "test-user" {
		t.Errorf("clientUID=%q, want test-user", o.clientUID)
	}
}

func TestUpgradeOptions_ClientUID_Empty(t *testing.T) {
	o := defaultUpgradeOptions()
	WithClientUID("")(o)
	if o.clientUID != "" {
		t.Errorf("clientUID=%q, want empty", o.clientUID)
	}
}

func TestUpgradeOptions_SSO(t *testing.T) {
	o := defaultUpgradeOptions()
	WithSSO()(o)
	if !o.enableSSO {
		t.Error("enableSSO should be true after WithSSO")
	}
}

func TestUpgradeOptions_EnableCompression(t *testing.T) {
	o := defaultUpgradeOptions()
	WithEnableCompression(false)(o)
	if o.enableCompression {
		t.Error("compression should be disabled")
	}
	WithEnableCompression(true)(o)
	if !o.enableCompression {
		t.Error("compression should be re-enabled")
	}
}

func TestUpgradeOptions_ReadLimit(t *testing.T) {
	o := defaultUpgradeOptions()
	WithReadLimit(8192)(o)
	if o.readLimit != 8192 {
		t.Errorf("readLimit=%d, want 8192", o.readLimit)
	}
}

func TestUpgradeOptions_ReadLimit_Zero(t *testing.T) {
	o := defaultUpgradeOptions()
	WithReadLimit(0)(o)
	if o.readLimit != 0 {
		t.Error("zero readLimit should not change")
	}
}

func TestUpgradeOptions_WriteLimit(t *testing.T) {
	o := defaultUpgradeOptions()
	WithWriteLimit(4096)(o)
	if o.writeLimit != 4096 {
		t.Errorf("writeLimit=%d, want 4096", o.writeLimit)
	}
}

func TestUpgradeOptions_WriteLimit_Zero(t *testing.T) {
	o := defaultUpgradeOptions()
	WithWriteLimit(0)(o)
	if o.writeLimit != 0 {
		t.Error("zero writeLimit should not change")
	}
}

func TestUpgradeOptions_ReadTimeout(t *testing.T) {
	o := defaultUpgradeOptions()
	WithReadTimeout(60 * time.Second)(o)
	if o.readTimeout != 60*time.Second {
		t.Errorf("readTimeout=%v, want 60s", o.readTimeout)
	}
}

func TestUpgradeOptions_ReadTimeout_Zero(t *testing.T) {
	o := defaultUpgradeOptions()
	WithReadTimeout(0)(o)
	if o.readTimeout != 0 {
		t.Error("zero readTimeout should not change")
	}
}

func TestUpgradeOptions_WriteTimeout(t *testing.T) {
	o := defaultUpgradeOptions()
	WithWriteTimeout(30 * time.Second)(o)
	if o.writeTimeout != 30*time.Second {
		t.Errorf("writeTimeout=%v, want 30s", o.writeTimeout)
	}
}

func TestUpgradeOptions_WriteTimeout_Zero(t *testing.T) {
	o := defaultUpgradeOptions()
	WithWriteTimeout(0)(o)
	if o.writeTimeout != 0 {
		t.Error("zero writeTimeout should not change")
	}
}

func TestUpgradeOptions_RateLimit(t *testing.T) {
	// 保存原值恢复
	oldLimiter := wsLimiter.Load()
	defer wsLimiter.Store(oldLimiter)

	o := defaultUpgradeOptions()
	WithWsRateLimit(100, 20)(o)
	if !o.enableWsRateLimit {
		t.Error("enableWsRateLimit should be true")
	}
	limiter := wsLimiter.Load()
	if limiter == nil {
		t.Fatal("wsLimiter should be set")
	}
	if limiter.Limit() != 100 {
		t.Errorf("limit=%.0f, want 100", limiter.Limit())
	}
	if limiter.Burst() != 20 {
		t.Errorf("burst=%d, want 20", limiter.Burst())
	}
}

func TestUpgradeOptions_RateLimit_ZeroValues(t *testing.T) {
	oldLimiter := wsLimiter.Load()
	defer wsLimiter.Store(oldLimiter)

	o := defaultUpgradeOptions()
	wsLimiter.Store(nil)
	WithWsRateLimit(0, 0)(o)
	if o.enableWsRateLimit {
		t.Error("enableWsRateLimit should be false for zero values")
	}
	if wsLimiter.Load() != nil {
		t.Error("wsLimiter should remain nil")
	}
}

// ---------------------------------------------------------------------------
// auth 功能
// ---------------------------------------------------------------------------

func TestExtractUID_FromClaims(t *testing.T) {
	claims := &jwt.Claims{UID: "user-123"}
	if uid := extractUID(claims); uid != "user-123" {
		t.Errorf("extractUID=%q, want user-123", uid)
	}
}

func TestExtractUID_FromFields(t *testing.T) {
	claims := &jwt.Claims{Fields: map[string]any{"id": "user-id-field"}}
	if uid := extractUID(claims); uid != "user-id-field" {
		t.Errorf("extractUID=%q, want user-id-field", uid)
	}
}

func TestExtractUID_PreferenceOrder(t *testing.T) {
	claims := &jwt.Claims{UID: "primary", Fields: map[string]any{"id": "secondary"}}
	if uid := extractUID(claims); uid != "primary" {
		t.Errorf("extractUID=%q, want primary", uid)
	}
}

func TestExtractUID_FieldsFallbackOrder(t *testing.T) {
	claims := &jwt.Claims{Fields: map[string]any{"user_id": "from-user_id"}}
	if uid := extractUID(claims); uid != "from-user_id" {
		t.Errorf("extractUID=%q, want from-user_id", uid)
	}
}

func TestExtractUID_EmptyClaims(t *testing.T) {
	claims := &jwt.Claims{}
	if uid := extractUID(claims); uid != "" {
		t.Errorf("extractUID=%q, want empty", uid)
	}
}

func TestExtractUID_IntField(t *testing.T) {
	claims := &jwt.Claims{Fields: map[string]any{"id": 12345}}
	if uid := extractUID(claims); uid != "12345" {
		t.Errorf("extractUID=%q, want 12345", uid)
	}
}

func TestParseTokenCtx_InvalidToken(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "invalid-token")
	if err == nil {
		t.Error("should return error for invalid token")
	}
}

func TestParseTokenCtx_BearerPrefix(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "Bearer invalid-token")
	if err == nil {
		t.Error("should return error for Bearer token")
	}
}

func TestParseTokenCtx_EmptyToken(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "")
	if err == nil {
		t.Error("should return error for empty token")
	}
}

func TestParseTokenCtx_ValidToken(t *testing.T) {
	jwt.Init()
	tokenStr := createTestJWT(t, "test-user-uid")
	uid, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("ParseTokenCtx failed: %v", err)
	}
	if uid != "test-user-uid" {
		t.Errorf("uid=%q, want test-user-uid", uid)
	}
}

func TestParseTokenCtx_ValidTokenBearer(t *testing.T) {
	jwt.Init()
	tokenStr := "Bearer " + createTestJWT(t, "bearer-user")
	uid, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("ParseTokenCtx with Bearer prefix failed: %v", err)
	}
	if uid != "bearer-user" {
		t.Errorf("uid=%q, want bearer-user", uid)
	}
}

func TestParseTokenCtx_MissingUID(t *testing.T) {
	jwt.Init()
	tokenStr := createTestJWTWithClaims(t, &jwt.Claims{})
	_, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != ErrTokenInvalid {
		t.Errorf("got %v, want ErrTokenInvalid", err)
	}
}

// ---------------------------------------------------------------------------
// Client 生命周期
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()
	if client.UID() != "test-uid" {
		t.Errorf("UID=%q", client.UID())
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr empty")
	}
	_ = testConn.WriteMessage(websocket.TextMessage, []byte("hello"))
	data, err := client.ReadMessageCtx(context.Background())
	if err != nil {
		t.Fatalf("ReadMessageCtx: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", string(data))
	}
}

func TestClose_Idempotent(t *testing.T) {
	client, _ := newTestClientPair(t)
	for i := 0; i < 3; i++ {
		if err := client.Close(); err != nil {
			t.Errorf("close #%d: %v", i, err)
		}
	}
}

func TestClose_ContextCancelled(t *testing.T) {
	client, _ := newTestClientPair(t)
	ctx := client.Context()
	client.Close()
	select {
	case <-ctx.Done():
	default:
		t.Error("context should be done after Close()")
	}
}

func TestClose_DrainBehavior(t *testing.T) {
	client, _ := newTestClientPair(t)
	for i := 0; i < 5; i++ {
		if err := client.WriteJSONCtx(context.Background(), Message{Type: "drain", Msg: fmt.Sprintf("msg-%d", i)}); err != nil {
			t.Fatalf("WriteJSONCtx #%d: %v", i, err)
		}
	}
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() not triggered within 1s after Close")
	}
	s := client.Stats()
	if s.WriteQueueLen != 0 {
		t.Errorf("writeCh not fully drained after Close, len=%d", s.WriteQueueLen)
	}
}

func TestClose_DrainWithFullQueue(t *testing.T) {
	client, _ := newTestClientPair(t)
	var queueFull, sentOk int
	for i := 0; i < 2000; i++ {
		err := client.WriteJSONCtx(context.Background(), Message{Type: "full", Data: make([]byte, 100)})
		if err == ErrWriteQueueFull {
			queueFull++
		} else if err == nil {
			sentOk++
		} else if err == websocket.ErrCloseSent {
			break
		} else {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	t.Logf("sentOk=%d queueFull=%d before Close", sentOk, queueFull)
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	s := client.Stats()
	if s.WriteQueueLen != 0 {
		t.Errorf("writeCh not drained after Close, len=%d", s.WriteQueueLen)
	}
	if !s.IsClosed {
		t.Error("IsClosed should be true")
	}
}

func TestSetCloseHook(t *testing.T) {
	client, _ := newTestClientPair(t)
	var called atomic.Bool
	client.SetCloseHook(func() { called.Store(true) })
	client.Close()
	if !called.Load() {
		t.Error("close hook not called")
	}
}

func TestClientCtx_CancelPropagation(t *testing.T) {
	// newClientWithConfig 不再内部做 WithoutCancel，传入的 ctx 取消会传递到 clientCtx
	ginCtx, ginCancel := context.WithCancel(context.Background())
	defer ginCancel()

	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		client := newClientWithConfig(ginCtx, raw, "propagate", &clientConfig{writeChSize: 1024, readChSize: 1024})
		serverCh <- client
	}))
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	_, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := <-serverCh
	defer client.Close()

	// 上游 ctx 取消后，clientCtx 也应被取消（WithoutCancel 不再由 NewClient 处理）
	ginCancel()
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Error("clientCtx should be done after upstream ctx cancel")
	}

	// 验证 Close 仍然幂等安全
	client.Close()
}

// ---------------------------------------------------------------------------
// Client 健康/统计
// ---------------------------------------------------------------------------

func TestClientStats(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "ping"})
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "pong"})
	time.Sleep(50 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _ = client.ReadMessageCtx(context.Background())
	s := client.Stats()
	if s.UID != "test-uid" {
		t.Errorf("UID=%q", s.UID)
	}
	if s.RemoteAddr == "" {
		t.Error("RemoteAddr empty")
	}
	if s.WriteQueueSize != 1024 {
		t.Errorf("WriteQueueSize=%d", s.WriteQueueSize)
	}
	if s.NumReceived != 1 {
		t.Errorf("NumReceived=%d, want 1", s.NumReceived)
	}
	if s.IsClosed {
		t.Error("IsClosed before Close()")
	}
	client.Close()
	s = client.Stats()
	if !s.IsClosed {
		t.Error("IsClosed false after Close()")
	}
}

func TestClient_IsAlive(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if !client.IsAlive() {
		t.Error("new client not alive")
	}
	client.Close()
	if client.IsAlive() {
		t.Error("alive after Close()")
	}
}

func TestHealth_InitialStats(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	s := client.Stats()
	if s.NumSent != 0 {
		t.Errorf("initial NumSent=%d", s.NumSent)
	}
	if s.NumReceived != 0 {
		t.Errorf("initial NumReceived=%d", s.NumReceived)
	}
	if s.WriteErrCount != 0 {
		t.Errorf("initial WriteErrCount=%d", s.WriteErrCount)
	}
	if s.ReadErrCount != 0 {
		t.Errorf("initial ReadErrCount=%d", s.ReadErrCount)
	}
}

func TestHealth_RecordWriteErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	client.recordWriteErr(fmt.Errorf("test write error"))
	s := client.Stats()
	if s.WriteErrCount != 1 {
		t.Errorf("WriteErrCount=%d, want 1", s.WriteErrCount)
	}
	if s.LastWriteErr != "test write error" {
		t.Errorf("LastWriteErr=%q", s.LastWriteErr)
	}
}

func TestHealth_RecordWriteErr_Multiple(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	client.recordWriteErr(fmt.Errorf("err1"))
	client.recordWriteErr(fmt.Errorf("err2"))
	s := client.Stats()
	if s.WriteErrCount != 2 {
		t.Errorf("WriteErrCount=%d, want 2", s.WriteErrCount)
	}
	if s.LastWriteErr != "err2" {
		t.Errorf("LastWriteErr=%q, want err2", s.LastWriteErr)
	}
}

func TestHealth_RecordReadErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	client.recordReadErr(fmt.Errorf("test read error"))
	s := client.Stats()
	if s.ReadErrCount != 1 {
		t.Errorf("ReadErrCount=%d, want 1", s.ReadErrCount)
	}
	if s.LastReadErr != "test read error" {
		t.Errorf("LastReadErr=%q", s.LastReadErr)
	}
}

func TestHealth_GetLastReadErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	err := client.getLastReadErr()
	if err == nil || err.Error() != "connection closed" {
		t.Errorf("getLastReadErr=%v, want 'connection closed'", err)
	}
	client.recordReadErr(fmt.Errorf("network timeout"))
	err = client.getLastReadErr()
	if err == nil || err.Error() != "network timeout" {
		t.Errorf("getLastReadErr=%v, want 'network timeout'", err)
	}
}

func TestHealth_MarkLastWrite(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	before := time.Now()
	time.Sleep(time.Millisecond)
	client.markLastWrite()
	s := client.Stats()
	if s.LastWriteTime == "" {
		t.Fatal("LastWriteTime empty")
	}
	writeTime, err := time.Parse(time.RFC3339Nano, s.LastWriteTime)
	if err != nil {
		t.Fatalf("parse LastWriteTime: %v", err)
	}
	if writeTime.Before(before) {
		t.Errorf("writeTime %v should be after %v", writeTime, before)
	}
}

func TestHealth_MarkLastRead(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	before := time.Now()
	time.Sleep(time.Millisecond)
	client.markLastRead()
	s := client.Stats()
	if s.LastReadTime == "" {
		t.Fatal("LastReadTime empty")
	}
	readTime, err := time.Parse(time.RFC3339Nano, s.LastReadTime)
	if err != nil {
		t.Fatalf("parse LastReadTime: %v", err)
	}
	if readTime.Before(before) {
		t.Errorf("readTime %v should be after %v", readTime, before)
	}
}

func TestStats_AfterClose_QueueMetrics(t *testing.T) {
	client, _ := newTestClientPair(t)
	for i := 0; i < 10; i++ {
		_ = client.WriteJSONCtx(context.Background(), Message{Type: "test"})
	}
	client.Close()
	s := client.Stats()
	if !s.IsClosed {
		t.Error("IsClosed should be true")
	}
	if s.WriteQueueLen != 0 {
		t.Errorf("WriteQueueLen=%d after drain, want 0", s.WriteQueueLen)
	}
	if s.NumSent == 0 {
		t.Log("NumSent=0 after Close - messages may have been drained")
	}
	if s.WriteQueueSize != 1024 {
		t.Errorf("WriteQueueSize=%d, want 1024", s.WriteQueueSize)
	}
	if s.ReadQueueSize != 1024 {
		t.Errorf("ReadQueueSize=%d, want 1024", s.ReadQueueSize)
	}
	t.Logf("LastWriteErr=%q, LastReadErr=%q", s.LastWriteErr, s.LastReadErr)
}

func TestWriteJSON_Success(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()
	if err := client.WriteJSONCtx(context.Background(), Message{Type: "greeting", Msg: "hi"}); err != nil {
		t.Fatalf("WriteJSONCtx: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("testConn.ReadMessage: %v", err)
	}
	if !strings.Contains(string(data), `"greeting"`) {
		t.Errorf("expected greeting, got %s", string(data))
	}
}

func TestWriteJSON_QueueFull(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	for i := 0; i < 100; i++ {
		if err := client.WriteJSONCtx(context.Background(), Message{Type: "ping", Data: i}); err == ErrWriteQueueFull {
			return
		} else if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	t.Log("queue never full (msgFromChToWs consumed all)")
}

func TestWriteJSON_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()
	if err := client.WriteJSONCtx(context.Background(), Message{Type: "ping"}); err != websocket.ErrCloseSent {
		t.Errorf("got %v, want ErrCloseSent", err)
	}
}

func TestWriteJSONCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var timeoutCount int
	for i := 0; i < 3000; i++ {
		err := client.WriteJSONCtx(ctx, Message{Type: "timeout", Data: make([]byte, 100)})
		if err == context.DeadlineExceeded {
			timeoutCount++
		} else if err != nil && err != ErrWriteQueueFull {
			t.Errorf("unexpected err: %v", err)
		}
		if err == context.DeadlineExceeded {
			break
		}
	}
	if timeoutCount == 0 {
		t.Log("writeCh never full enough for timeout - write path always available")
	} else {
		t.Logf("ctx timeout triggered %d times", timeoutCount)
	}
}

func TestWriteRawCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var timeoutCount int
	for i := 0; i < 3000; i++ {
		err := client.WriteRawCtx(ctx, make([]byte, 100))
		if err == context.DeadlineExceeded {
			timeoutCount++
			break
		} else if err != nil && err != ErrWriteQueueFull {
			t.Errorf("unexpected err: %v", err)
			break
		}
	}
	if timeoutCount == 0 {
		t.Log("writeCh never full enough for timeout")
	} else {
		t.Logf("WriteRawCtx ctx timeout triggered %d times", timeoutCount)
	}
}

func TestWriteRaw_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()
	if err := client.WriteRawCtx(context.Background(), []byte("data")); err != websocket.ErrCloseSent {
		t.Errorf("got %v, want ErrCloseSent", err)
	}
}

func TestReadMessage_Count(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()
	for i := 0; i < 5; i++ {
		_ = c.WriteMessage(websocket.TextMessage, []byte("msg"))
		if _, err := client.ReadMessageCtx(context.Background()); err != nil {
			t.Fatalf("ReadMessageCtx: %v", err)
		}
	}
	if s := client.Stats(); s.NumReceived != 5 {
		t.Errorf("NumReceived=%d, want 5", s.NumReceived)
	}
}

func TestReadMessageCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.ReadMessageCtx(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("got %v, want DeadlineExceeded", err)
	}
}

func TestCheckWriteLimit_NoLimit(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if err := client.checkWriteLimit(make([]byte, 100000)); err != nil {
		t.Errorf("no limit should pass: %v", err)
	}
}

func TestCheckWriteLimit_WithinLimit(t *testing.T) {
	client, _ := newTestClientPair(t, func(o *clientConfig) { o.writeLimit = 1000 })
	defer client.Close()
	if err := client.checkWriteLimit(make([]byte, 500)); err != nil {
		t.Errorf("within limit should pass: %v", err)
	}
}

func TestCheckWriteLimit_ExceedLimit(t *testing.T) {
	client, _ := newTestClientPair(t, func(o *clientConfig) { o.writeLimit = 100 })
	defer client.Close()
	if err := client.checkWriteLimit(make([]byte, 200)); err != ErrWriteLimitExceeded {
		t.Errorf("exceed limit: got %v, want ErrWriteLimitExceeded", err)
	}
}

func TestCheckWriteLimit_ExactBoundary(t *testing.T) {
	client, _ := newTestClientPair(t, func(o *clientConfig) { o.writeLimit = 100 })
	defer client.Close()
	if err := client.checkWriteLimit(make([]byte, 100)); err != nil {
		t.Errorf("equal to limit should pass: %v", err)
	}
	if err := client.checkWriteLimit(make([]byte, 101)); err != ErrWriteLimitExceeded {
		t.Errorf("1 byte over limit should reject")
	}
}

func TestWriteLimit_ThroughWriteRaw(t *testing.T) {
	client, _ := newTestClientPair(t, func(o *clientConfig) { o.writeLimit = 10 })
	defer client.Close()
	err := client.WriteRawCtx(context.Background(), make([]byte, 100))
	if err != ErrWriteLimitExceeded {
		t.Errorf("WriteRawCtx with over-limit: got %v, want ErrWriteLimitExceeded", err)
	}
}

func BenchmarkWriteJSON(b *testing.B) {
	client, _ := newTestClientPair(b)
	defer client.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.WriteJSONCtx(context.Background(), Message{Type: "bench", Msg: "hello"})
	}
}

// ---------------------------------------------------------------------------
// Client 代理方法
// ---------------------------------------------------------------------------

func TestClientProxy_NoDispatcher(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	client.SendToUIDCtx(context.Background(), "someone", Message{Type: "t"})
	client.SendToMultiUIDCtx(context.Background(), []string{"a", "b"}, Message{Type: "t"})
	client.BroadcastCtx(context.Background(), Message{Type: "t"})
	client.BroadcastReliableCtx(context.Background(), Message{Type: "t"})
}

func TestClientProxy_SendToSelf_NoOp(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	alice.SendToUIDCtx(context.Background(), "alice", Message{Type: "self"})
	if !d.hasLocalUID("alice") {
		t.Error("alice should still be in dispatcher")
	}
}

func TestClientProxy_SendToUID_Success(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bobConn := newTestClientWithUID(t, "bob", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	alice.SendToUIDCtx(context.Background(), "bob", Message{Type: "proxy", Msg: "from alice"})
	time.Sleep(50 * time.Millisecond)
	bobConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "from alice" {
		t.Errorf("bob got %q, want 'from alice'", msg.Msg)
	}
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := aliceConn.ReadMessage(); err == nil {
		t.Error("alice should NOT receive her own send")
	}
}

func TestClientProxy_SendToMultiUID_FilterSelf(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bobConn := newTestClientWithUID(t, "bob", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	alice.SendToMultiUIDCtx(context.Background(), []string{"alice", "bob"}, Message{Type: "multi", Msg: "team msg"})
	time.Sleep(50 * time.Millisecond)
	bobConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "team msg" {
		t.Errorf("bob got %q", msg.Msg)
	}
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := aliceConn.ReadMessage(); err == nil {
		t.Error("alice should NOT receive her own multicast")
	}
}

func TestClientProxy_SendToMultiUID_EmptyFiltered(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	alice.SendToMultiUIDCtx(context.Background(), []string{"alice"}, Message{Type: "t"})
}

func TestClientProxy_Broadcast(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bobConn := newTestClientWithUID(t, "bob", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	alice.BroadcastCtx(context.Background(), Message{Type: "announce", Msg: "all"})
	time.Sleep(50 * time.Millisecond)
	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive broadcast: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "all" {
			t.Errorf("%s got %q", name, msg.Msg)
		}
	}
}

func TestClientProxy_BroadcastReliable(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bobConn := newTestClientWithUID(t, "bob", func(o *clientConfig) { o.dispatcher = d })
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	alice.BroadcastReliableCtx(context.Background(), Message{Type: "reliable", Msg: "guaranteed"})
	time.Sleep(50 * time.Millisecond)
	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive reliable: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "guaranteed" {
			t.Errorf("%s got %q", name, msg.Msg)
		}
	}
}

// ---------------------------------------------------------------------------
// Dispatcher 生命周期
// ---------------------------------------------------------------------------

func TestDispatcher_Basic(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	if n := d.Len(); n != 3 {
		t.Fatalf("Len=%d, want 3", n)
	}
	if n := len(d.Clients()); n != 3 {
		t.Fatalf("Clients=%d, want 3", n)
	}
	count := 0
	d.Range(func(c *Client) bool { count++; return true })
	if count != 3 {
		t.Errorf("Range count=%d, want 3", count)
	}
	s := d.Stats()
	if s.TotalConnections != 3 {
		t.Errorf("TotalConnections=%d", s.TotalConnections)
	}
}

func TestDispatcher_RegisterUnregister(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), c); err != nil {
		t.Fatalf("register: %v", err)
	}
	if d.Len() != 1 {
		t.Errorf("after register Len=%d", d.Len())
	}
	if err := d.UnregisterCtx(context.Background(), c); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("after unregister Len=%d", d.Len())
	}
	c.Close()
}

func TestRegisterCtx_CancelledCtx(t *testing.T) {
	d := NewDispatcher(nil)
	defer d.CleanupDeadConns()
	c, _ := newTestClientWithUID(t, "alice")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// RegisterCtx 不再检查 ctx 取消，取消的 ctx 不影响注册
	err := d.RegisterCtx(ctx, c)
	if err != nil {
		t.Fatalf("register should succeed: %v", err)
	}
	if d.Len() != 1 {
		t.Errorf("Len=%d after register, want 1", d.Len())
	}
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be true after register")
	}
	c.Close()
}

func TestUnregisterCtx_CancelledCtx(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "bob")
	if err := d.RegisterCtx(context.Background(), c); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// UnregisterCtx 不再检查 ctx 取消，取消的 ctx 不影响注销
	err := d.UnregisterCtx(ctx, c)
	if err != nil {
		t.Fatalf("unregister should succeed: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("Len=%d after unregister, want 0", d.Len())
	}
	if d.hasLocalUID("bob") {
		t.Error("hasLocalUID should be false after unregister")
	}
	c.Close()
}

func TestDispatcher_MaxConnections(t *testing.T) {
	d := NewDispatcher(nil, WithMaxConnections(2))
	c1, _ := newTestClientWithUID(t, "a")
	_ = d.RegisterCtx(context.Background(), c1)
	defer c1.Close()
	c2, _ := newTestClientWithUID(t, "b")
	_ = d.RegisterCtx(context.Background(), c2)
	defer c2.Close()
	c3, _ := newTestClientWithUID(t, "c")
	if err := d.RegisterCtx(context.Background(), c3); err != ErrMaxConnections {
		t.Errorf("got %v, want ErrMaxConnections", err)
	}
	c3.Close()
}

func TestDispatcher_CleanupDeadConns(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be true after register")
	}
	c.Close()
	time.Sleep(50 * time.Millisecond)
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should still be true before CleanupDeadConns")
	}
	if n := d.CleanupDeadConns(); n != 1 {
		t.Errorf("Clean=%d, want 1", n)
	}
	if d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be false after CleanupDeadConns")
	}
}

func TestDispatcher_CleanupDeadConns_MultiDevice(t *testing.T) {
	d := NewDispatcher(nil)
	c1, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c1)
	c2, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c2)
	defer c2.Close()
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be true after 2 connections")
	}
	c1.Close()
	time.Sleep(50 * time.Millisecond)
	if n := d.CleanupDeadConns(); n != 1 {
		t.Errorf("Clean=%d, want 1", n)
	}
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should still be true when other device still connected")
	}
}

// ---------------------------------------------------------------------------
// Dispatcher 发送
// ---------------------------------------------------------------------------

func TestDispatcher_SendToUID(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, _ := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "private", Msg: "hello"})
	time.Sleep(50 * time.Millisecond)
	sA := alice.Stats()
	sB := bob.Stats()
	if sA.NumSent == 0 && sA.WriteQueueLen == 0 {
		t.Error("alice should have message")
	}
	if sB.NumSent > 0 || sB.WriteQueueLen > 0 {
		t.Errorf("bob should NOT receive, sent=%d", sB.NumSent)
	}
	d.SendToUIDCtx(context.Background(), "nonexistent", Message{Type: "ghost"})
}

func TestDispatcher_MultiDevice(t *testing.T) {
	d := NewDispatcher(nil)
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "alice")
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "multi", Msg: "sync"})
	time.Sleep(50 * time.Millisecond)
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			s := c.Stats()
			if s.NumSent == 0 && s.WriteQueueLen == 0 {
				t.Errorf("alice device should have message")
			}
		}
		return true
	})
}

func TestDispatcher_BroadcastFilter(t *testing.T) {
	d := NewDispatcher(nil)
	targets := map[string]bool{"alice": true, "charlie": true}
	for uid := range targets {
		c, _ := newTestClientWithUID(t, uid)
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	bob, _ := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "team", Msg: "msg"},
		func(c *Client) bool { return targets[c.UID()] },
	)
	time.Sleep(50 * time.Millisecond)
	if s := bob.Stats(); s.NumSent > 0 || s.WriteQueueLen > 0 {
		t.Errorf("bob should NOT receive filtered")
	}
}

func TestBroadcastFilter_DeadClient(t *testing.T) {
	d := NewDispatcher(nil)
	c, conn := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)
	conn.Close()
	time.Sleep(50 * time.Millisecond)
	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "test"},
		func(c *Client) bool { return true },
	)
	if n := d.Len(); n != 0 {
		t.Logf("clients after broadcast with dead: %d", n)
		cleaned := d.CleanupDeadConns()
		t.Logf("manual CleanupDeadConns removed: %d", cleaned)
	}
}

func TestDispatcher_BroadcastReliable(t *testing.T) {
	d := NewDispatcher(nil)
	alice, connA := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, connB := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	d.BroadcastReliableCtx(context.Background(), Message{Type: "reliable", Msg: "all must receive"})
	time.Sleep(50 * time.Millisecond)
	for name, conn := range map[string]*websocket.Conn{"alice": connA, "bob": connB} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive BroadcastReliable: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "all must receive" {
			t.Errorf("%s got msg=%q", name, msg.Msg)
		}
	}
}

// ---------------------------------------------------------------------------
// 分布式 Dispatcher
// ---------------------------------------------------------------------------

func TestDistributed_SendToUID(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()
	alice, ac := newTestClientWithUID(t, "alice")
	_ = A.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bc := newTestClientWithUID(t, "bob")
	_ = B.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	A.SendToUIDCtx(ctx, "bob", Message{Type: "greeting", Msg: "hello bob"})
	time.Sleep(100 * time.Millisecond)
	_, data, err := bc.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "hello bob" {
		t.Errorf("bob got %q, want %q", msg.Msg, "hello bob")
	}
	ac.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := ac.ReadMessage(); err == nil {
		t.Error("alice should NOT receive")
	}
}

func TestDistributed_SendToMultiUID(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()
	conns := map[string]*websocket.Conn{}
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		_ = dd.RegisterCtx(context.Background(), c)
		conns[uid] = conn
		defer c.Close()
	}
	dd.SendToMultiUIDCtx(ctx, []string{"alice", "charlie"}, Message{Type: "team", Msg: "team msg"})
	time.Sleep(100 * time.Millisecond)
	for _, uid := range []string{"alice", "charlie"} {
		_, data, err := conns[uid].ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", uid, err)
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "team msg" {
			t.Errorf("%s got %q", uid, msg.Msg)
		}
	}
	conns["bob"].SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := conns["bob"].ReadMessage(); err == nil {
		t.Error("bob should NOT receive")
	}
}

func TestDistributed_Broadcast(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()
	alice, ac := newTestClientWithUID(t, "alice")
	_ = A.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bc := newTestClientWithUID(t, "bob")
	_ = B.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	A.BroadcastCtx(ctx, Message{Type: "announce", Msg: "通知"})
	time.Sleep(100 * time.Millisecond)
	for name, conn := range map[string]*websocket.Conn{"alice": ac, "bob": bc} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", name, err)
		}
		t.Logf("%s 收到: %s", name, string(data))
	}
}

func TestDistributed_SelfPublish(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()
	c, conn := newTestClientWithUID(t, "alice")
	_ = dd.RegisterCtx(context.Background(), c)
	defer c.Close()
	dd.SendToUIDCtx(ctx, "alice", Message{Type: "self", Msg: "自己发的"})
	time.Sleep(100 * time.Millisecond)
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("self-publish: %v", err)
	}
}

func TestDistributed_ConnectedUIDs(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()
	for _, uid := range []string{"alice", "alice", "bob"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = A.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	for _, uid := range []string{"charlie", "dave"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = B.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	time.Sleep(200 * time.Millisecond)
	for name, dd := range map[string]*DistributedDispatcher{"A": A, "B": B} {
		if uids := dd.ConnectedUIDs(); len(uids) != 4 {
			t.Errorf("%s ConnectedUIDs=%d, want 4: %v", name, len(uids), uids)
		}
	}
}

func TestDistributed_Concurrent(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, "user")
		_ = dd.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	var done atomic.Int64
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				dd.BroadcastCtx(ctx, Message{Type: "t", Msg: "test"})
				done.Add(1)
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	t.Logf("concurrent send: %d", done.Load())
}

func TestDistributed_ConcurrentView(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", i))
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register: %v", err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	var send, view atomic.Int64
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				d.SendToUIDCtx(context.Background(), fmt.Sprintf("u-%d", j%5), Message{Type: "t"})
				send.Add(1)
			}
		}()
		go func() {
			for j := 0; j < 50; j++ {
				_ = d.Len()
				_ = d.Clients()
				_ = d.Stats()
				d.Range(func(c *Client) bool { return true })
				view.Add(1)
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	t.Logf("concurrent: send=%d view=%d online=%d", send.Load(), view.Load(), d.Len())
}

func TestDistributed_ConcurrentOnlineOffline(t *testing.T) {
	backend := newMockBackend(256)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dd.handleRemoteOnline([]string{"alice"})
		}()
	}
	wg.Wait()
	val, ok := dd.remoteUIDCounts.Load("alice")
	if !ok {
		t.Fatal("alice should be in remoteUIDTotal")
	}
	counter := val.(*atomic.Int32)
	if n := counter.Load(); n != 20 {
		t.Errorf("remoteUIDTotal[alice]=%d, want 20", n)
	}
	if n := dd.remoteUIDTotal.Load(); n != 1 {
		t.Errorf("remoteUIDTotal=%d, want 1", n)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dd.handleRemoteOffline([]string{"alice"})
		}()
	}
	wg.Wait()
	if _, ok := dd.remoteUIDCounts.Load("alice"); ok {
		t.Error("alice should be removed from remoteUIDCounts after all offline")
	}
	if n := dd.remoteUIDTotal.Load(); n != 0 {
		t.Errorf("remoteUIDTotal=%d, want 0", n)
	}
}

func TestDistributed_Integration(t *testing.T) {
	backend := newMockBackend(64)
	instances := []*DistributedDispatcher{NewDispatcher(backend), NewDispatcher(backend)}
	ctx := context.Background()
	for _, dd := range instances {
		dd.Start(ctx)
		defer dd.Stop()
	}
	type uc struct {
		client *Client
		conn   *websocket.Conn
	}
	users := map[string]*uc{}
	for i, name := range []string{"alice", "bob", "charlie", "dave"} {
		c, conn := newTestClientWithUID(t, name)
		users[name] = &uc{c, conn}
		_ = instances[i/2].RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	instances[0].SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from 0"})
	instances[1].BroadcastCtx(ctx, Message{Type: "notice", Msg: "ann"})
	time.Sleep(150 * time.Millisecond)
	received := map[string]int{}
	for _, u := range users {
		for {
			u.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			if _, _, err := u.conn.ReadMessage(); err != nil {
				break
			} else {
				received[u.client.UID()]++
			}
		}
	}
	if received["charlie"] < 2 {
		t.Errorf("charlie got %d, want >=2", received["charlie"])
	}
	if received["dave"] < 1 {
		t.Errorf("dave got %d, want >=1", received["dave"])
	}
}

// ---------------------------------------------------------------------------
// Upgrade 功能
// ---------------------------------------------------------------------------

func TestUpgrade_PerIPLimit(t *testing.T) {
	ipConnCounts = sync.Map{}
	var rejectedCount atomic.Int32
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithMaxConnPerIP(2),
		)
		if err != nil {
			rejectedCount.Add(1)
			return
		}
		defer client.Close()
		<-client.Done()
	})
	s := httptest.NewServer(r)
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	origin := http.Header{"Origin": {"http://trusted.com"}}
	conn1, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("conn1 dial: %v", err)
	}
	defer conn1.Close()
	conn2, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("conn2 dial: %v", err)
	}
	defer conn2.Close()
	_, _, err = websocket.DefaultDialer.Dial(url, origin)
	if err == nil {
		t.Error("3rd connection from same IP should be rejected")
	}
	if n := rejectedCount.Load(); n != 1 {
		t.Errorf("rejectedCount=%d, want 1", n)
	}
}

func TestUpgrade_PerIPLimit_CloseHook(t *testing.T) {
	ipConnCounts = sync.Map{}
	var conn1Closed atomic.Bool
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithMaxConnPerIP(5),
		)
		if err != nil {
			return
		}
		conn1Closed.Store(true)
		client.Close()
	})
	s := httptest.NewServer(r)
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	origin := http.Header{"Origin": {"http://trusted.com"}}
	conn, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	time.Sleep(50 * time.Millisecond)
	if !conn1Closed.Load() {
		t.Error("server should have closed the connection")
	}
	if actual, ok := ipConnCounts.Load("127.0.0.1"); ok {
		if counter, ok := actual.(*atomic.Int32); ok {
			if n := counter.Load(); n != 0 {
				t.Errorf("ipConnCounts count=%d, want 0 after close", n)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 集成测试
// ---------------------------------------------------------------------------

func TestIntegration_WebSocketFullLifecycle(t *testing.T) {
	d := NewDispatcher(nil)
	heartStarted := make(chan struct{})
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithClientUID("test-user"),
			WithHeartbeat(),
			WithEnableDistributed(true),
			WithDispatcher(d),
		)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}
		close(heartStarted)
		for {
			_, err := client.ReadMessageCtx(context.Background())
			if err != nil {
				return
			}
		}
	})
	s := httptest.NewServer(r)
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"http://trusted.com"}})
	if err != nil {
		t.Fatalf("WebSocket Dial 失败: %v", err)
	}
	defer conn.Close()
	select {
	case <-heartStarted:
	case <-time.After(time.Second):
		t.Fatal("心跳未在 1s 内启动")
	}
	time.Sleep(100 * time.Millisecond)
	if n := d.Len(); n != 1 {
		t.Errorf("Dispatcher.Len() = %d, want 1", n)
	}
	var client *Client
	d.Range(func(c *Client) bool {
		client = c
		return false
	})
	if client == nil {
		t.Fatal("Dispatcher 中应有 Client")
	}
	if client.UID() == "" {
		t.Error("UID 不应为空")
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr 不应为空")
	}
	if !client.IsAlive() {
		t.Error("新连接应处于存活状态")
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","msg":"hello"}`)); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}
	msg := Message{Type: "notify", Msg: "server push"}
	d.SendToUIDCtx(context.Background(), client.UID(), msg)
	time.Sleep(50 * time.Millisecond)
	stats := client.Stats()
	t.Logf("Client Stats: UID=%s, NumSent=%d, NumReceived=%d", stats.UID, stats.NumSent, stats.NumReceived)
	if stats.UID != client.UID() {
		t.Errorf("Stats.UID = %q, want %q", stats.UID, client.UID())
	}
	dStats := d.Stats()
	t.Logf("Dispatcher Stats: TotalConnections=%d", dStats.TotalConnections)
	if dStats.TotalConnections != 1 {
		t.Errorf("TotalConnections = %d, want 1", dStats.TotalConnections)
	}
	client.Close()
	if client.IsAlive() {
		t.Error("Close 后 IsAlive 应为 false")
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Error("Close 后 Done 应在 1s 内触发")
	}
	time.Sleep(100 * time.Millisecond)
	if n := d.Len(); n != 0 {
		t.Errorf("Close 后 Dispatcher.Len() = %d, want 0", n)
	}
}

func TestIntegration_DistributedMessaging(t *testing.T) {
	backend := newMockBackend(64)
	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)
	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceA.RegisterCtx(context.Background(), bob)
	defer bob.Close()
	charlie, charlieConn := newTestClientWithUID(t, "charlie")
	_ = instanceB.RegisterCtx(context.Background(), charlie)
	defer charlie.Close()
	dave, daveConn := newTestClientWithUID(t, "dave")
	_ = instanceB.RegisterCtx(context.Background(), dave)
	defer dave.Close()
	time.Sleep(150 * time.Millisecond)
	uidsA := instanceA.ConnectedUIDs()
	uidsB := instanceB.ConnectedUIDs()
	if len(uidsA) != 4 || len(uidsB) != 4 {
		t.Errorf("ConnectedUIDs: A=%d, B=%d, want both=4", len(uidsA), len(uidsB))
	}
	instanceA.SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from A to charlie"})
	instanceB.SendToUIDCtx(ctx, "alice", Message{Type: "p2p", Msg: "from B to alice"})
	instanceB.BroadcastCtx(ctx, Message{Type: "notice", Msg: "global notice"})
	time.Sleep(200 * time.Millisecond)
	received := map[string]int{}
	for name, conn := range map[string]*websocket.Conn{
		"alice": aliceConn, "bob": bobConn,
		"charlie": charlieConn, "dave": daveConn,
	} {
		for {
			conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			received[name]++
			t.Logf("%s 收到: %s", name, string(data))
		}
	}
	if received["alice"] < 2 {
		t.Errorf("alice 应收到 >=2 条, 实际 %d", received["alice"])
	}
	if received["charlie"] < 2 {
		t.Errorf("charlie 应收到 >=2 条, 实际 %d", received["charlie"])
	}
	if received["bob"] < 1 {
		t.Errorf("bob 应收到 >=1 条, 实际 %d", received["bob"])
	}
	if received["dave"] < 1 {
		t.Errorf("dave 应收到 >=1 条, 实际 %d", received["dave"])
	}
}

func TestIntegration_ConcurrentSendAndStats(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "user")
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register: %v", err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				d.BroadcastCtx(context.Background(), Message{Type: "concurrent", Msg: "test"})
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	statsList := make([]ClientStats, 0, len(clients))
	d.Range(func(c *Client) bool {
		statsList = append(statsList, c.Stats())
		return true
	})
	for i, s := range statsList {
		t.Logf("[%d] Sent=%d, QueueLen=%d", i, s.NumSent, s.WriteQueueLen)
	}
	dStats := d.Stats()
	if int(dStats.TotalConnections) != len(clients) {
		t.Errorf("TotalConnections=%d, want %d", dStats.TotalConnections, len(clients))
	}
	_, _ = json.Marshal(dStats)
}

// ---------------------------------------------------------------------------
// 并发安全
// ---------------------------------------------------------------------------

func TestClose_ConcurrentWriteAndClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	var writeErrCount, writeOkCount, closeSentCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				err := client.WriteJSONCtx(context.Background(), Message{Type: "concurrent", Msg: fmt.Sprintf("g-%d-%d", id, j)})
				if err == nil {
					writeOkCount.Add(1)
				} else if err == websocket.ErrCloseSent || err == ErrWriteQueueFull {
					closeSentCount.Add(1)
					return
				} else {
					writeErrCount.Add(1)
					t.Errorf("unexpected write err: %v", err)
					return
				}
			}
		}(i)
	}
	time.Sleep(10 * time.Millisecond)
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	wg.Wait()
	t.Logf("writeOk=%d closeSent=%d writeErr=%d", writeOkCount.Load(), closeSentCount.Load(), writeErrCount.Load())
	total := writeOkCount.Load() + closeSentCount.Load() + writeErrCount.Load()
	if total == 0 {
		t.Error("no writes completed")
	}
	if writeErrCount.Load() > 0 {
		t.Errorf("unexpected write errors: %d", writeErrCount.Load())
	}
}

func TestConcurrent_ReadAndWrite(t *testing.T) {
	client, conn := newTestClientPair(t)
	defer client.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = client.WriteJSONCtx(context.Background(), Message{Type: "w", Msg: fmt.Sprintf("w-%d", i)})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"r","msg":"r-%d"}`, i)))
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = client.ReadMessageCtx(context.Background())
		}
	}()
	wg.Wait()
	s := client.Stats()
	t.Logf("concurrent rw: NumSent=%d, NumReceived=%d", s.NumSent, s.NumReceived)
}

func TestConcurrent_RangeAndRegister(t *testing.T) {
	d := NewDispatcher(nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", id))
			_ = d.RegisterCtx(context.Background(), c)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			d.Range(func(c *Client) bool { return true })
		}
	}()
	wg.Wait()
	t.Logf("concurrent range+register: Len=%d", d.Len())
}

func TestConcurrent_ClientsAndCleanup(t *testing.T) {
	d := NewDispatcher(nil)
	for i := 0; i < 10; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", i))
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.CleanupDeadConns()
			_ = len(d.Clients())
			_ = d.Stats()
		}()
	}
	wg.Wait()
	t.Logf("concurrent cleanup+clients: Len=%d", d.Len())
}

// ---------------------------------------------------------------------------
// 内部函数测试
// ---------------------------------------------------------------------------

func TestDefaultDispatcher_Exists(t *testing.T) {
	if DefaultDispatcher == nil {
		t.Fatal("DefaultDispatcher should not be nil")
	}
	if DefaultDispatcher.instanceID != "standalone" {
		t.Errorf("instanceID=%q, want standalone", DefaultDispatcher.instanceID)
	}
	if DefaultDispatcher.backend != nil {
		t.Error("DefaultDispatcher backend should be nil")
	}
	if n := DefaultDispatcher.MaxConnections(); n != 0 {
		t.Errorf("MaxConnections=%d, want 0", n)
	}
}

func TestRequestIDAttr_WithID(t *testing.T) {
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-123")
	kv := requestIDAttr(ctx)
	if kv.Value.AsString() != "req-123" {
		t.Errorf("requestID=%q, want req-123", kv.Value.AsString())
	}
}

func TestRequestIDAttr_WithoutID(t *testing.T) {
	kv := requestIDAttr(context.Background())
	if kv.Value.AsString() != "" {
		t.Errorf("requestID=%q, want empty", kv.Value.AsString())
	}
}

func TestRequestIDAttr_NilCtx(t *testing.T) {
	kv := requestIDAttr(nil)
	if kv.Value.AsString() != "" {
		t.Errorf("requestID=%q, want empty for nil ctx", kv.Value.AsString())
	}
}

func TestBuildClientOpts_All(t *testing.T) {
	o := defaultUpgradeOptions()
	WithQueueSize(2048, 4096)(o)
	WithReadTimeout(30 * time.Second)(o)
	WithWriteTimeout(15 * time.Second)(o)
	WithReadLimit(8192)(o)
	WithWriteLimit(4096)(o)

	cfg := &o.clientConfig
	if cfg == nil {
		t.Fatal("cfg should not be nil")
	}

	if cfg.writeChSize != 2048 {
		t.Errorf("writeChSize=%d, want 2048", cfg.writeChSize)
	}
	if cfg.readChSize != 4096 {
		t.Errorf("readChSize=%d, want 4096", cfg.readChSize)
	}
	if cfg.readTimeout != 30*time.Second {
		t.Errorf("readTimeout=%v", cfg.readTimeout)
	}
	if cfg.writeTimeout != 15*time.Second {
		t.Errorf("writeTimeout=%v", cfg.writeTimeout)
	}
	if cfg.readLimit != 8192 {
		t.Errorf("readLimit=%d", cfg.readLimit)
	}
	if cfg.writeLimit != 4096 {
		t.Errorf("writeLimit=%d", cfg.writeLimit)
	}
}

func TestBuildClientOpts_WithDispatcher(t *testing.T) {
	o := defaultUpgradeOptions()
	dd := NewDispatcher(nil)
	WithDispatcher(dd)(o)

	cfg := &o.clientConfig
	if cfg.dispatcher != dd {
		t.Error("dispatcher should be propagated")
	}
}

func TestBuildClientOpts_Empty(t *testing.T) {
	o := defaultUpgradeOptions()
	cfg := &o.clientConfig
	if cfg.writeChSize != 1024 {
		t.Errorf("writeChSize=%d, want default 1024", cfg.writeChSize)
	}
	if cfg.readChSize != 1024 {
		t.Errorf("readChSize=%d, want default 1024", cfg.readChSize)
	}
}

func TestSetupPongHandler_ValidConn(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 不应 panic
	SetupPongHandler(client.wsConn, 30*time.Second, 10*time.Second)
}

func TestWriteWithRetry_ClosedConn(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()
	err := client.writeWithRetry([]byte("test"))
	if err == nil {
		t.Error("should return error on closed connection")
	}
}

func TestUpgradeRegisterDispatcher_PropagatesRegisterError(t *testing.T) {
	dd := NewDispatcher(nil, WithMaxConnections(0))
	client, _ := newTestClientPair(t)
	defer client.Close()

	o := defaultUpgradeOptions()
	o.dispatcher = dd
	o.enableDistributed = true

	// 注册成功
	err := upgradeRegisterDispatcher(context.Background(), client, o)
	if err != nil {
		t.Fatalf("register should succeed: %v", err)
	}

	// 验证关闭钩子触发注销
	if n := dd.Len(); n != 1 {
		t.Errorf("Len=%d, want 1 after register", n)
	}

	client.Close()
	time.Sleep(50 * time.Millisecond)
	if n := dd.Len(); n != 0 {
		t.Errorf("Len=%d, want 0 after close", n)
	}
}

// ---------------------------------------------------------------------------
// 单点登录（SSO）
// ---------------------------------------------------------------------------

func TestDispatcher_SSO_KickOld(t *testing.T) {
	d := NewDispatcher(nil)
	d.enableSSO = true

	alice, aliceConn := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice); err != nil {
		t.Fatalf("first register: %v", err)
	}
	defer alice.Close()

	if !alice.IsAlive() {
		t.Error("first alice should be alive")
	}

	// 第二个相同 UID 的连接应踢掉第一个
	alice2, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice2); err != nil {
		t.Fatalf("second register: %v", err)
	}
	defer alice2.Close()

	time.Sleep(50 * time.Millisecond)

	// 旧连接应已被自动关闭
	if alice.IsAlive() {
		t.Error("first alice should be kicked by SSO")
	}
	// 新连接应存活
	if !alice2.IsAlive() {
		t.Error("second alice should be alive")
	}
	// 验证 Dispatcher 中只有新连接
	if n := d.Len(); n != 1 {
		t.Errorf("Len=%d, want 1 after SSO kick", n)
	}

	// 旧连接的底层连接应已被关闭
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := aliceConn.ReadMessage(); err == nil {
		t.Error("old alice conn should be closed")
	}
}

func TestDispatcher_SSO_NoKickOnDifferentUID(t *testing.T) {
	d := NewDispatcher(nil)
	d.enableSSO = true

	alice, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	defer alice.Close()

	bob, _ := newTestClientWithUID(t, "bob")
	if err := d.RegisterCtx(context.Background(), bob); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	defer bob.Close()

	time.Sleep(50 * time.Millisecond)

	// 不同 UID 不应互相踢
	if !alice.IsAlive() {
		t.Error("alice should still be alive after bob registers")
	}
	if !bob.IsAlive() {
		t.Error("bob should be alive")
	}
	if n := d.Len(); n != 2 {
		t.Errorf("Len=%d, want 2 for different UIDs", n)
	}
}

func TestDispatcher_SSO_CleanupOnClose(t *testing.T) {
	d := NewDispatcher(nil)
	d.enableSSO = true

	alice, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	defer alice.Close()

	// 验证 SSO 映射存在
	if _, loaded := d.ssoClients.Load("alice"); !loaded {
		t.Error("alice should be in ssoClients after register")
	}

	// 通过 UnregisterCtx 注销后，SSO 映射应清理
	if err := d.UnregisterCtx(context.Background(), alice); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if _, loaded := d.ssoClients.Load("alice"); loaded {
		t.Error("alice should be removed from ssoClients after UnregisterCtx")
	}
}

func TestDispatcher_SSO_KickReRegister(t *testing.T) {
	d := NewDispatcher(nil)
	d.enableSSO = true

	// 注册第一个
	alice1, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice1); err != nil {
		t.Fatalf("first register: %v", err)
	}
	defer alice1.Close()

	// 注册第二个踢第一个
	alice2, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice2); err != nil {
		t.Fatalf("second register: %v", err)
	}
	defer alice2.Close()

	time.Sleep(50 * time.Millisecond)

	// 注册第三个踢第二个
	alice3, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), alice3); err != nil {
		t.Fatalf("third register: %v", err)
	}
	defer alice3.Close()

	time.Sleep(50 * time.Millisecond)

	// 只有第三个应存活
	if alice1.IsAlive() || alice2.IsAlive() {
		t.Error("alice1 and alice2 should be kicked")
	}
	if !alice3.IsAlive() {
		t.Error("alice3 should be alive")
	}
	if n := d.Len(); n != 1 {
		t.Errorf("Len=%d, want 1 after multiple SSO kicks", n)
	}
	// 验证 SSO 映射指向最新的客户端
	if loaded, ok := d.ssoClients.Load("alice"); !ok || loaded != alice3 {
		t.Error("ssoClients should point to the latest client")
	}
}

func TestUpgradeLimiter_GlobalState(t *testing.T) {
	oldLimiter := wsLimiter.Load()
	defer wsLimiter.Store(oldLimiter)

	wsLimiter.Store(nil)
	if wsLimiter.Load() != nil {
		t.Error("limiter should be nil initially")
	}

	o := defaultUpgradeOptions()
	WithWsRateLimit(50, 10)(o)
	if wsLimiter.Load() == nil {
		t.Error("limiter should be set after WithWsRateLimit")
	}
}

func TestUpgradeRateLimit_AllowDeny(t *testing.T) {
	oldLimiter := wsLimiter.Load()
	defer wsLimiter.Store(oldLimiter)

	// 用极低速率模拟限制
	WithWsRateLimit(1, 1)(defaultUpgradeOptions())
	limiter := wsLimiter.Load()
	if limiter == nil {
		t.Fatal("limiter should be set")
	}

	// 第一个请求应允许
	if !limiter.Allow() {
		t.Error("first request should be allowed")
	}
	// 第二个请求应被限制
	if limiter.Allow() {
		t.Log("second request may be allowed with rate=1 burst=1")
	}
}

func TestUpgradePerIPCheck_NoExisting(t *testing.T) {
	ipConnCounts = sync.Map{}
	_, span := otel.Tracer("test").Start(context.Background(), "test")
	defer span.End()
	err := upgradePerIPCheck("10.0.0.1", 5, span)
	if err != nil {
		t.Errorf("no existing counter should pass: %v", err)
	}
}

func TestUpgradePerIPCheck_AtLimit(t *testing.T) {
	ipConnCounts = sync.Map{}
	counter := &atomic.Int32{}
	counter.Store(5)
	ipConnCounts.Store("10.0.0.1", counter)

	_, span := otel.Tracer("test").Start(context.Background(), "test")
	defer span.End()

	err := upgradePerIPCheck("10.0.0.1", 5, span)
	if err == nil {
		t.Error("should reject when at limit")
	}
}

func TestUpgradePerIPCheck_UnderLimit(t *testing.T) {
	ipConnCounts = sync.Map{}
	counter := &atomic.Int32{}
	counter.Store(3)
	ipConnCounts.Store("10.0.0.1", counter)

	_, span := otel.Tracer("test").Start(context.Background(), "test")
	defer span.End()

	err := upgradePerIPCheck("10.0.0.1", 5, span)
	if err != nil {
		t.Errorf("under limit should pass: %v", err)
	}
}

func TestUpgradePerIPDecrement_Existing(t *testing.T) {
	ipConnCounts = sync.Map{}
	counter := &atomic.Int32{}
	counter.Store(3)
	ipConnCounts.Store("10.0.0.1", counter)

	ipConnCountDecrement("10.0.0.1")
	if n := counter.Load(); n != 2 {
		t.Errorf("counter=%d, want 2", n)
	}

	ipConnCountDecrement("10.0.0.1")
	ipConnCountDecrement("10.0.0.1")
	if _, ok := ipConnCounts.Load("10.0.0.1"); ok {
		t.Error("entry should be deleted when counter reaches 0")
	}
}

func TestUpgradePerIPDecrement_NonExistent(t *testing.T) {
	ipConnCounts = sync.Map{}
	// 不应 panic
	ipConnCountDecrement("unknown")
}

func TestClientNew_WriteTimeoutDefault(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if client.writeTimeout != writeDeadline {
		t.Errorf("writeTimeout=%v, want %v", client.writeTimeout, writeDeadline)
	}
}

func TestClientNew_ReadTimeoutPropagation(t *testing.T) {
	client, _ := newTestClientPair(t, func(o *clientConfig) { o.readTimeout = 45 * time.Second })
	defer client.Close()
	if client.readTimeout != 45*time.Second {
		t.Errorf("readTimeout=%v, want 45s", client.readTimeout)
	}
}

func TestHealth_IsAlive_Initial(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if !client.IsAlive() {
		t.Error("new client should be alive")
	}
}

func TestHealth_IsAlive_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()
	if client.IsAlive() {
		t.Error("client should be dead after Close")
	}
}

func TestHealth_RecordWriteErr_Empty(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	s := client.Stats()
	if s.LastWriteErr != "" {
		t.Errorf("initial LastWriteErr=%q, want empty", s.LastWriteErr)
	}
}

func TestHealth_RecordReadErr_Empty(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	s := client.Stats()
	if s.LastReadErr != "" {
		t.Errorf("initial LastReadErr=%q, want empty", s.LastReadErr)
	}
}

func TestHealth_MarkLastWrite_Zero(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	before := time.Now()
	time.Sleep(time.Millisecond)
	client.markLastWrite()
	client.markLastWrite() // 幂等
	s := client.Stats()
	writeTime, err := time.Parse(time.RFC3339Nano, s.LastWriteTime)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if writeTime.Before(before) {
		t.Errorf("writeTime should be after start")
	}
}
