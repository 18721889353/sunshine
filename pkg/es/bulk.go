package es

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elastic/go-elasticsearch/v7/esapi"
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
func (b *Bulk) BulkExecute(ctx context.Context, operations []BulkOperation) error {
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

		actionLine, err := json.Marshal(action)
		if err != nil {
			endSpan(err)
			return fmt.Errorf("marshal action error: %w", err)
		}

		buf.Write(actionLine)
		buf.WriteByte('\n')

		if op.Payload != nil && op.Action != "delete" {
			payload, err := json.Marshal(op.Payload)
			if err != nil {
				endSpan(err)
				return fmt.Errorf("marshal payload error: %w", err)
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
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("bulk operation failed: %s", res.String())
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
