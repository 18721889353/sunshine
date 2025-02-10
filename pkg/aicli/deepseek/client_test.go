package deepseek

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var apiKey = "sk-xxxxxx"

func TestClient_Send(t *testing.T) {
	client, err := NewClient(apiKey)
	if err != nil {
		t.Log(err)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	mdContent, err := client.Send(ctx, "你是谁？")
	if err != nil {
		t.Log(err)
		return
	}
	t.Log(mdContent)
}

func TestClient_SendStream(t *testing.T) {
	client, err := NewClient(apiKey,
		WithModel(ModelDeepSeekReasoner),
		WithMaxTokens(8192),
		WithTemperature(TemperatureCodeGeneration),
		WithUseContext(true),
		WithRole(RoleTypeGeneral),
	)
	if err != nil {
		t.Log(err)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	answer := client.SendStream(ctx, "你使用的是哪个模型回答问题？")
	for content := range answer.Content {
		fmt.Printf(content)
	}
	if answer.Err != nil {
		t.Log(answer.Err)
		return
	}
}

func TestClient_ListModelNames(t *testing.T) {
	client, err := NewClient(apiKey)
	if err != nil {
		t.Log(err)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	modelNames, err := client.ListModelNames(ctx)
	if err != nil {
		t.Log(err)
		return
	}
	t.Log(modelNames)
}
