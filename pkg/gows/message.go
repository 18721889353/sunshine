package gows

// Message WebSocket 服务端通用消息结构。
// 所有服务端推送消息统一使用此结构序列化，
// 客户端通过 Type 字段区分消息类型进行分发处理。
type Message struct {
	Type string `json:"type"`           // 消息类型标识（如 "ping", "notify", "greeting"）
	Msg  string `json:"msg,omitempty"`  // 消息内容文本（可选）
	Data any    `json:"data,omitempty"` // 附加业务数据（可选，任意类型）
}
