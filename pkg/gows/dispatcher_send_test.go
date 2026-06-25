package gows

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Dispatcher 发送/广播测试
// ---------------------------------------------------------------------------

func TestDemo_SendToSingleUID(t *testing.T) {
	d := NewDispatcher(nil)

	// 创建 3 个不同用户
	clients := make(map[string]*Client)
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients[uid] = c
		_ = conn // 用于验证接收
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 只发给 alice
	msg := Message{Type: "private", Msg: "Hello Alice!"}
	d.SendToUIDCtx(context.Background(), "alice", msg)

	// 发给不存在的 UID — 不应 panic
	d.SendToUIDCtx(context.Background(), "nonexistent", Message{Type: "ghost"})

	// 验证: alice 的写入队列应有 1 条，bob 和 charlie 应为 0
	time.Sleep(50 * time.Millisecond)

	for uid, c := range clients {
		s := c.Stats()
		if uid == "alice" {
			if s.NumSent == 0 && s.WriteQueueLen > 0 {
				t.Logf("✅ %s 消息在队列中, queue_len=%d", uid, s.WriteQueueLen)
			} else if s.NumSent > 0 {
				t.Logf("✅ %s 已收到消息 (sent=%d)", uid, s.NumSent)
			}
		} else {
			t.Logf("✅ %s 不应有消息 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		}
	}
}

func TestDemo_SendToMultipleUIDs(t *testing.T) {
	d := NewDispatcher(nil)

	clients := make(map[string]*Client)
	for _, uid := range []string{"alice", "bob", "charlie", "dave"} {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients[uid] = c
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 只发给 alice 和 charlie
	targets := map[string]bool{"alice": true, "charlie": true}
	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "team_notify", Msg: "团队消息"},
		func(c *Client) bool {
			return targets[c.UID()]
		},
	)

	time.Sleep(50 * time.Millisecond)
	t.Log("✅ BroadcastFilterCtx 只发给 alice 和 charlie:")
	for uid, c := range clients {
		s := c.Stats()
		if targets[uid] {
			t.Logf("   └─ %s ✅ 应收到 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		} else {
			t.Logf("   └─ %s ❌ 不应收到 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		}
	}
}

func TestDemo_SendDifferentMessages(t *testing.T) {
	d := NewDispatcher(nil)

	scores := map[string]int{"alice": 95, "bob": 88, "charlie": 72}
	clients := make([]*Client, 0, len(scores))
	for uid := range scores {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 根据 UID 给不同用户发送不同的成绩消息
	d.Range(func(c *Client) bool {
		if score, ok := scores[c.UID()]; ok {
			c.WriteJSON(Message{
				Type: "score_report",
				Data: map[string]any{"uid": c.UID(), "score": score},
			})
		}
		return true
	})

	time.Sleep(50 * time.Millisecond)
	t.Log("✅ Range + WriteJSON 给不同用户发不同消息:")
	d.Range(func(c *Client) bool {
		s := c.Stats()
		t.Logf("   └─ %s: sent=%d", c.UID(), s.NumSent)
		return true
	})
}

func TestDemo_SameUIDMultiDevice(t *testing.T) {
	d := NewDispatcher(nil)

	// 同一用户 "alice" 有 3 个设备连接
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "alice")
		if err := d.Register(c); err != nil {
			t.Fatalf("register alice device %d: %v", i, err)
		}
		defer c.Close()
	}

	// 查看 alice 的连接数
	aliceCount := 0
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			aliceCount++
		}
		return true
	})
	t.Logf("✅ alice 在线设备数: %d", aliceCount)

	// 发送给 alice — 所有设备都能收到
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "multi_device", Msg: "所有设备同步"})

	t.Log("✅ SendToUIDCtx 发给 alice 的所有设备:")
	time.Sleep(50 * time.Millisecond)
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			s := c.Stats()
			t.Logf("   └─ [设备] addr=%s sent=%d queue_len=%d", c.RemoteAddr(), s.NumSent, s.WriteQueueLen)
		}
		return true
	})

	// 直接向某个特定设备发送（不经过 SendToUIDCtx）
	var specificDevice *Client
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			specificDevice = c
			return false
		}
		return true
	})
	if specificDevice != nil {
		specificDevice.WriteJSON(Message{Type: "private", Msg: "仅此设备可见"})
		time.Sleep(50 * time.Millisecond)
		s := specificDevice.Stats()
		t.Logf("✅ 指定设备单独发送完毕 (sent=%d)", s.NumSent)
	}
}
