package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/elastic/go-elasticsearch/v7/esapi"
)

// Document 文档操作接口
type Document struct {
	client *Client
}

// NewDocument 创建文档操作实例
func (c *Client) Document() *Document {
	return &Document{client: c}
}

// Index 索引文档
func (d *Document) Index(ctx context.Context, index string, doc interface{}, docID string) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "index", index, docID)
	defer endSpan(nil)

	// 使用缓冲池优化内存分配
	body := d.client.getBuffer()
	defer d.client.putBuffer(body)

	if err := json.NewEncoder(body).Encode(doc); err != nil {
		endSpan(err)
		return fmt.Errorf("marshal document error: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      index,
		DocumentID: docID,
		Body:       bytes.NewReader(body.Bytes()),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("index document error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("index document failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// Get 获取文档
func (d *Document) Get(ctx context.Context, index string, docID string, result interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "get", index, docID)
	defer endSpan(nil)

	req := esapi.GetRequest{
		Index:      index,
		DocumentID: docID,
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("get document error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == 404 {
		err = fmt.Errorf("document not found")
		endSpan(err)
		return err
	}

	if res.IsError() {
		err = fmt.Errorf("get document failed: %s", res.String())
		endSpan(err)
		return err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("read response body error: %w", err)
	}

	var response struct {
		Source json.RawMessage `json:"_source"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		endSpan(err)
		return fmt.Errorf("unmarshal response error: %w", err)
	}

	if err := json.Unmarshal(response.Source, result); err != nil {
		endSpan(err)
		return fmt.Errorf("unmarshal document error: %w", err)
	}

	return nil
}

// Delete 删除文档
func (d *Document) Delete(ctx context.Context, index string, docID string) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "delete", index, docID)
	defer endSpan(nil)

	req := esapi.DeleteRequest{
		Index:      index,
		DocumentID: docID,
		Refresh:    "true",
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("delete document error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("delete document failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// Update 添加更新文档功能
func (d *Document) Update(ctx context.Context, index string, docID string, updateData interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "update", index, docID, updateData)
	defer endSpan(nil)

	// 使用缓冲池优化内存分配
	body := d.client.getBuffer()
	defer d.client.putBuffer(body)

	if err := json.NewEncoder(body).Encode(map[string]interface{}{
		"doc": updateData,
	}); err != nil {
		endSpan(err)
		return fmt.Errorf("marshal update data error: %w", err)
	}

	req := esapi.UpdateRequest{
		Index:      index,
		DocumentID: docID,
		Body:       bytes.NewReader(body.Bytes()),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("update document error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("update document failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// BulkIndex 添加批量操作示例
func (d *Document) BulkIndex(ctx context.Context, index string, docs []map[string]interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "bulk_index", index, docs)
	defer endSpan(nil)

	return d.client.Bulk().BulkIndex(ctx, index, docs)
}

// BulkCreate 批量创建文档
func (d *Document) BulkCreate(ctx context.Context, index string, docs []map[string]interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "bulk_create", index, docs)
	defer endSpan(nil)

	return d.client.Bulk().BulkCreate(ctx, index, docs)
}

// BulkUpdate 批量更新文档
func (d *Document) BulkUpdate(ctx context.Context, index string, updates []map[string]interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "bulk_update", index, updates)
	defer endSpan(nil)

	return d.client.Bulk().BulkUpdate(ctx, index, updates)
}

// BulkDelete 批量删除文档
func (d *Document) BulkDelete(ctx context.Context, index string, ids []string) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "bulk_delete", index, ids)
	defer endSpan(nil)

	return d.client.Bulk().BulkDelete(ctx, index, ids)
}

// IndexExists 检查索引是否存在
func (d *Document) IndexExists(ctx context.Context, index string) (bool, error) {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "index_exists", index)
	defer endSpan(nil)

	res, err := d.client.Indices.Exists([]string{index}, d.client.Indices.Exists.WithContext(ctx))
	if err != nil {
		endSpan(err)
		return false, fmt.Errorf("check index exists error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	switch res.StatusCode {
	case 200:
		return true, nil
	case 404:
		return false, nil
	default:
		err = fmt.Errorf("check index exists failed: %s", res.String())
		endSpan(err)
		return false, err
	}
}

// DeleteIndex 删除索引
func (d *Document) DeleteIndex(ctx context.Context, index string) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "delete_index", index)
	defer endSpan(nil)

	res, err := d.client.Indices.Delete([]string{index}, d.client.Indices.Delete.WithContext(ctx))
	if err != nil {
		endSpan(err)
		return fmt.Errorf("delete index error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("delete index failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// CreateIndex 创建索引
func (d *Document) CreateIndex(ctx context.Context, index string, mapping interface{}) error {
	// 添加追踪支持
	ctx, endSpan := d.client.withSpan(ctx, "create_index", index, mapping)
	defer endSpan(nil)

	var body io.Reader
	if mapping != nil {
		// 使用缓冲池优化内存分配
		buf := d.client.getBuffer()
		defer d.client.putBuffer(buf)

		if err := json.NewEncoder(buf).Encode(mapping); err != nil {
			endSpan(err)
			return fmt.Errorf("marshal mapping error: %w", err)
		}
		body = bytes.NewReader(buf.Bytes())
	}

	res, err := d.client.Indices.Create(
		index,
		d.client.Indices.Create.WithContext(ctx),
		d.client.Indices.Create.WithBody(body),
	)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("create index error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("create index failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}
