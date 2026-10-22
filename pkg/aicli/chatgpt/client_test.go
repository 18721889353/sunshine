package gptclient

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

var apiKey = "sk-xxxxxx"

func TestClient_Send(t *testing.T) {
	client, err := NewClient(apiKey, WithModel(ModelGPT4oMini), WithTemperature(0.0), WithRole(RoleTypeGeneral))
	if err != nil {
		t.Log(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	replyContent, err := client.Send(ctx, "Who are you?")
	if err != nil {
		t.Log(err)
		return
	}
	t.Log(replyContent)
}

func TestClient_SendStream(t *testing.T) {
	client, err := NewClient(apiKey,
		WithModel(ModelGPT4o),
		WithMaxTokens(8192),
		WithUseContext(true),
	)
	if err != nil {
		t.Log(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply := client.SendStream(ctx, "Which model did you use to answer the question?")
	for content := range reply.Content {
		fmt.Printf("%s", content)
	}
	if reply.Err != nil {
		t.Log(reply.Err)
		return
	}
}

func TestClient_ListModelNames(t *testing.T) {
	client, err := NewClient(apiKey)
	if err != nil {
		t.Log(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	modelNames, err := client.ListModelNames(ctx)
	if err != nil {
		t.Log(err)
		return
	}
	t.Log(modelNames)
}

// TestNewRequestTokenLimitField 守护输出上限的字段口径：
// 请求体只能带 max_completion_tokens，不得再带已弃用的 max_tokens——
// 两者互斥，同时发送会被 OpenAI 判为 400（unsupported_parameter）。
func TestNewRequestTokenLimitField(t *testing.T) {
	client := defaultClientOptions()
	client.apply(WithModel(ModelGPT4o), WithMaxTokens(8192))

	tests := []struct {
		name       string
		stream     bool
		wantStream bool
	}{
		{name: "同步路径", stream: false, wantStream: false},
		{name: "流式路径", stream: true, wantStream: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(client.newRequest("hello", tt.stream))
			if err != nil {
				t.Fatalf("序列化请求体失败: %v", err)
			}

			var fields map[string]json.RawMessage
			if err = json.Unmarshal(body, &fields); err != nil {
				t.Fatalf("解析请求体失败: %v", err)
			}

			if raw, ok := fields["max_tokens"]; ok {
				t.Errorf("请求体不应包含已弃用的 max_tokens: %s", raw)
			}
			raw, ok := fields["max_completion_tokens"]
			if !ok {
				t.Fatalf("请求体缺少 max_completion_tokens: %s", body)
			}
			if string(raw) != "8192" {
				t.Errorf("max_completion_tokens = %s, want 8192", raw)
			}
			if got := fields["stream"]; (string(got) == "true") != tt.wantStream {
				t.Errorf("stream 与入参不符, wantStream=%v, body=%s", tt.wantStream, body)
			}
		})
	}
}

// TestWithMaxTokens 守护下限回退：低于 minMaxTokens 的误传不得原样写进请求，
// 否则输出预算为 0 级别，模型回复会被截成空内容。
func TestWithMaxTokens(t *testing.T) {
	tests := []struct {
		name string
		give int
		want int
	}{
		{name: "正常取值直接生效", give: 8192, want: 8192},
		{name: "下限边界生效", give: minMaxTokens, want: minMaxTokens},
		{name: "低于下限回退默认值", give: 1, want: defaultMaxTokens},
		{name: "零值回退默认值", give: 0, want: defaultMaxTokens},
		{name: "负值回退默认值", give: -100, want: defaultMaxTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := defaultClientOptions()
			client.apply(WithMaxTokens(tt.give))
			if client.maxTokens != tt.want {
				t.Errorf("maxTokens = %d, want %d", client.maxTokens, tt.want)
			}
		})
	}
}
