package es

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v7/esapi"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Bulk 批量操作接口
type Bulk struct {
	client *Client
}

// BulkOperation 批量操作项
type BulkOperation struct {
	Index   string      `json:"_index"`
	ID      string      `json:"_id,omitempty"`
	Action  string      `json:"-"` // index, create, update, delete
	Payload interface{} `json:"-"`
}

// Bulk NewBulk 创建批量操作实例
func (c *Client) Bulk() *Bulk {
	return &Bulk{client: c}
}

// BulkExecute 执行批量操作
func (b *Bulk) BulkExecute(ctx context.Context, operations []BulkOperation) (err error) {
	// 指标埋点：ES 批量操作耗时（op, result）
	start := time.Now()
	defer func() { observeES("bulk", err, time.Since(start)) }()

	// 添加追踪支持
	ctx, endSpan := b.client.withSpan(ctx, "bulk_execute")
	defer endSpan(nil)

	// 使用缓冲池优化内存分配
	buf := b.client.getBuffer()
	defer b.client.putBuffer(buf)

	for _, op := range operations {
		meta := map[string]interface{}{
			"_index": op.Index,
		}

		// 只有当ID不为空时才添加_id字段
		if op.ID != "" {
			meta["_id"] = op.ID
		}

		action := map[string]interface{}{
			op.Action: meta,
		}

		// return fmt.Errorf 会先把值赋给命名返回值 err 再执行 defer，埋点可记到 error
		actionLine, marshalErr := json.Marshal(action)
		if marshalErr != nil {
			endSpan(marshalErr)
			return fmt.Errorf("marshal action error: %w", marshalErr)
		}

		buf.Write(actionLine)
		buf.WriteByte('\n')

		if op.Payload != nil && op.Action != "delete" {
			payload, marshalErr := json.Marshal(op.Payload)
			if marshalErr != nil {
				endSpan(marshalErr)
				return fmt.Errorf("marshal payload error: %w", marshalErr)
			}
			buf.Write(payload)
			buf.WriteByte('\n')
		}
	}

	req := esapi.BulkRequest{
		Body: buf,
	}

	res, err := req.Do(ctx, b.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("bulk operation error: %w", err)
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "关闭ES响应体失败", logger.Err(closeErr))
		}
	}()

	if res.IsError() {
		err = fmt.Errorf("bulk operation failed: %s", res.String())
		endSpan(err)
		return err
	}

	// partial failure：HTTP 200 但 errors=true（部分文档失败，如 mapping 冲突/
	// 版本冲突/非法字段）。ES bulk 的失败是"部分成功"，只看 HTTP 状态会把
	// 这类业务失败记成 ok——漏诊是监控的红线。
	var bulkResp struct {
		Errors bool                                `json:"errors"`
		Items  []map[string]map[string]interface{} `json:"items"`
	}
	if decErr := json.NewDecoder(res.Body).Decode(&bulkResp); decErr == nil && bulkResp.Errors {
		// 收集失败项（index/create/update/delete 各自带 status）
		var failed []string
		for _, item := range bulkResp.Items {
			for action, detail := range item {
				if st, ok := detail["status"].(float64); ok && st >= 400 {
					failed = append(failed, fmt.Sprintf("%s:%.0f", action, st))
				}
			}
		}
		if len(failed) == 0 {
			failed = append(failed, "errors=true(明细缺失)")
		}
		err = fmt.Errorf("bulk partial failure: %d/%d 项失败 (%s)",
			len(failed), len(bulkResp.Items), strings.Join(failed, ","))
		endSpan(err)
		return err
	}

	return nil
}

// BulkIndex 批量索引文档
func (b *Bulk) BulkIndex(ctx context.Context, index string, docs []map[string]interface{}) error {
	operations := make([]BulkOperation, len(docs))
	for i, doc := range docs {
		// 创建不包含_id字段的文档副本
		docCopy := make(map[string]interface{})
		for k, v := range doc {
			docCopy[k] = v
		}

		operation := BulkOperation{
			Index:   index,
			Action:  "index",
			Payload: docCopy,
		}

		// 检查文档中是否包含_id字段，如果有则使用它作为ID，并从文档副本中移除
		if docID, ok := doc["_id"]; ok {
			delete(docCopy, "_id") // 从文档内容中移除_id字段
			if strID, ok := docID.(string); ok && strID != "" {
				operation.ID = strID
			} else if intID, ok := docID.(int); ok {
				operation.ID = fmt.Sprintf("%d", intID)
			} else if floatID, ok := docID.(float64); ok {
				// 处理数字字符串偏好，将浮点数转换为整数字符串
				operation.ID = fmt.Sprintf("%.0f", floatID)
			}
		}

		operations[i] = operation
	}

	return b.BulkExecute(ctx, operations)
}

// BulkCreate 批量创建文档
func (b *Bulk) BulkCreate(ctx context.Context, index string, docs []map[string]interface{}) error {
	operations := make([]BulkOperation, len(docs))
	for i, doc := range docs {
		// 创建不包含_id字段的文档副本
		docCopy := make(map[string]interface{})
		for k, v := range doc {
			docCopy[k] = v
		}

		operation := BulkOperation{
			Index:   index,
			Action:  "create",
			Payload: docCopy,
		}

		// 检查文档中是否包含_id字段，如果有则使用它作为ID，并从文档副本中移除
		if docID, ok := doc["_id"]; ok {
			delete(docCopy, "_id") // 从文档内容中移除_id字段
			if strID, ok := docID.(string); ok && strID != "" {
				operation.ID = strID
			} else if intID, ok := docID.(int); ok {
				operation.ID = fmt.Sprintf("%d", intID)
			} else if floatID, ok := docID.(float64); ok {
				// 处理数字字符串偏好，将浮点数转换为整数字符串
				operation.ID = fmt.Sprintf("%.0f", floatID)
			}
		}

		operations[i] = operation
	}

	return b.BulkExecute(ctx, operations)
}

// BulkUpdate 批量更新文档
func (b *Bulk) BulkUpdate(ctx context.Context, index string, docs []map[string]interface{}) error {
	operations := make([]BulkOperation, len(docs))
	for i, doc := range docs {
		// 创建不包含_id字段的文档副本
		docCopy := make(map[string]interface{})
		for k, v := range doc {
			docCopy[k] = v
		}

		operation := BulkOperation{
			Index:   index,
			Action:  "update",
			Payload: map[string]interface{}{"doc": docCopy},
		}

		// 检查文档中是否包含_id字段，如果有则使用它作为ID，并从文档副本中移除
		if docID, ok := doc["_id"]; ok {
			delete(docCopy, "_id") // 从文档内容中移除_id字段
			if strID, ok := docID.(string); ok && strID != "" {
				operation.ID = strID
			} else if intID, ok := docID.(int); ok {
				operation.ID = fmt.Sprintf("%d", intID)
			} else if floatID, ok := docID.(float64); ok {
				// 处理数字字符串偏好，将浮点数转换为整数字符串
				operation.ID = fmt.Sprintf("%.0f", floatID)
			}
		}

		// 确保每个更新操作都有ID
		if operation.ID == "" {
			return fmt.Errorf("document at index %d missing _id field, update operation requires document ID", i)
		}

		operations[i] = operation
	}

	return b.BulkExecute(ctx, operations)
}

// BulkDelete 批量删除文档
func (b *Bulk) BulkDelete(ctx context.Context, index string, ids []string) error {
	operations := make([]BulkOperation, len(ids))
	for i, id := range ids {
		operations[i] = BulkOperation{
			Index:  index,
			ID:     id,
			Action: "delete",
		}
	}

	return b.BulkExecute(ctx, operations)
}

// MixedBulkExecute 混合批量操作
func (b *Bulk) MixedBulkExecute(ctx context.Context, indexOperations map[string][]BulkOperation) error {
	operations := make([]BulkOperation, 0)
	for _, ops := range indexOperations {
		operations = append(operations, ops...)
	}

	return b.BulkExecute(ctx, operations)
}
