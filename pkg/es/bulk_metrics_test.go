package es

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBulkPartialFailureDetection bulk 的 partial failure（HTTP 200 + errors=true）
// 必须被识别为错误——只看 HTTP 状态会把 mapping 冲突等业务失败记成 ok。
func TestBulkPartialFailureDetection(t *testing.T) {
	// 构造 ES partial failure 响应
	body := `{"took":30,"errors":true,"items":[{"index":{"_id":"1","status":400,"error":{"type":"mapper_parsing_exception"}}},{"index":{"_id":"2","status":201}}]}`
	var bulkResp struct {
		Errors bool                                `json:"errors"`
		Items  []map[string]map[string]interface{} `json:"items"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&bulkResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bulkResp.Errors {
		t.Fatal("应识别 errors=true")
	}
	// 统计失败项（status>=400）
	failed := 0
	for _, item := range bulkResp.Items {
		for _, detail := range item {
			if st, _ := detail["status"].(float64); st >= 400 {
				failed++
			}
		}
	}
	if failed != 1 {
		t.Errorf("应识别 1 个失败项, got %d", failed)
	}
}
