package gows

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 测试辅助工具定义在 test_helpers.go:
//   newTestClientPair / newTestClientWithUID / newTestServer

// ---------------------------------------------------------------------------
// TestNewClient
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	if client.UID() != "test-uid" {
		t.Errorf("UID = %q, want %q", client.UID(), "test-uid")
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr should not be empty")
	}

	// 验证 Done() 在未关闭前不返回
	select {
	case <-client.Done():
		t.Error("Done() channel should NOT be closed before Close()")
	default:
	}

	_ = testConn.WriteMessage(websocket.TextMessage, []byte("hello"))
	data, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", string(data), "hello")
	}
}

// ---------------------------------------------------------------------------
// TestClose 相关
// ---------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	client, _ := newTestClientPair(t)

	if err := client.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("third close: %v", err)
	}
}

func TestClose_ContextCancelled(t *testing.T) {
	client, _ := newTestClientPair(t)

	ctx := client.Context()
	select {
	case <-ctx.Done():
		t.Error("context should NOT be done before Close()")
	default:
	}

	client.Close()

	select {
	case <-ctx.Done():
		// expected
	default:
		t.Error("context should be done after Close()")
	}
}

func TestClose_DoneChannel(t *testing.T) {
	client, _ := newTestClientPair(t)

	done := client.Done()
	select {
	case <-done:
		t.Error("done should NOT fire before close")
	default:
	}

	client.Close()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Error("done should fire after close within 1s")
	}
}

// ---------------------------------------------------------------------------
// TestSetCloseHook
// ---------------------------------------------------------------------------

func TestSetCloseHook(t *testing.T) {
	client, _ := newTestClientPair(t)

	var hookCalled atomic.Bool
	client.SetCloseHook(func() {
		hookCalled.Store(true)
	})

	client.Close()

	if !hookCalled.Load() {
		t.Error("close hook was not called")
	}
}

// ---------------------------------------------------------------------------
// TestClient_RemoteAddr
// ---------------------------------------------------------------------------

func TestClient_RemoteAddr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	addr := client.RemoteAddr()
	if addr == "" {
		t.Error("RemoteAddr should not be empty")
	}
	t.Logf("RemoteAddr = %s", addr)
}
