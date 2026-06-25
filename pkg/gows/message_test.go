package gows

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestMessage_JSON
// ---------------------------------------------------------------------------

func TestMessageJSON(t *testing.T) {
	t.Parallel()

	msg := Message{
		Type: "notify",
		Msg:  "hello world",
		Data: map[string]int{"count": 42},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !strings.Contains(string(data), `"notify"`) {
		t.Errorf("expected type in json, got %s", string(data))
	}
	if !strings.Contains(string(data), `42`) {
		t.Errorf("expected data in json, got %s", string(data))
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if decoded.Type != "notify" {
		t.Errorf("Type = %q, want %q", decoded.Type, "notify")
	}
}
