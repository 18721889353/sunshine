package gptclient

import (
	"context"
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

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	reply := client.SendStream(ctx, "Which model did you use to answer the question?")
	for content := range reply.Content {
		fmt.Printf(content)
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

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	modelNames, err := client.ListModelNames(ctx)
	if err != nil {
		t.Log(err)
		return
	}
	t.Log(modelNames)
}
