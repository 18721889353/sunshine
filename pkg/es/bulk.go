package es

import (
	"bytes"
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

// NewBulk 创建批量操作实例
func (c *Client) Bulk() *Bulk {
	return &Bulk{client: c}
}

// BulkExecute 执行批量操作
func (b *Bulk) BulkExecute(ctx context.Context, operations []BulkOperation) error {
	// 添加追踪支持
	ctx, endSpan := b.client.withSpan(ctx, "bulk_execute")
	defer endSpan(nil)
	
	var buf bytes.Buffer

	for _, op := range operations {
		action := map[string]interface{}{
			op.Action: map[string]interface{}{
				"_index": op.Index,
				"_id":    op.ID,
			},
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
		Body: &buf,
	}

	res, err := req.Do(ctx, b.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("bulk operation error: %w", err)
	}
	defer res.Body.Close()

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
		operations[i] = BulkOperation{
			Index:   index,
			ID:      fmt.Sprintf("%d", i),
			Action:  "index",
			Payload: doc,
		}
	}
	
	return b.BulkExecute(ctx, operations)
}

// BulkCreate 批量创建文档
func (b *Bulk) BulkCreate(ctx context.Context, index string, docs []map[string]interface{}) error {
	operations := make([]BulkOperation, len(docs))
	for i, doc := range docs {
		operations[i] = BulkOperation{
			Index:   index,
			ID:      fmt.Sprintf("%d", i),
			Action:  "create",
			Payload: doc,
		}
	}
	
	return b.BulkExecute(ctx, operations)
}

// BulkUpdate 批量更新文档
func (b *Bulk) BulkUpdate(ctx context.Context, index string, updates map[string]interface{}) error {
	operations := make([]BulkOperation, 0, len(updates))
	for id, update := range updates {
		operations = append(operations, BulkOperation{
			Index:   index,
			ID:      id,
			Action:  "update",
			Payload: map[string]interface{}{"doc": update},
		})
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
