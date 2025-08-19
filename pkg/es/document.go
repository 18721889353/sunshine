package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/elastic/go-elasticsearch/v8/esapi"
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
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal document error: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      index,
		DocumentID: docID,
		Body:       bytes.NewReader(body),
		Refresh:    "true",
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		return fmt.Errorf("index document error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("index document failed: %s", res.String())
	}

	return nil
}

// Get 获取文档
func (d *Document) Get(ctx context.Context, index string, docID string, result interface{}) error {
	req := esapi.GetRequest{
		Index:      index,
		DocumentID: docID,
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		return fmt.Errorf("get document error: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		return fmt.Errorf("document not found")
	}

	if res.IsError() {
		return fmt.Errorf("get document failed: %s", res.String())
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read response body error: %w", err)
	}

	var response struct {
		Source json.RawMessage `json:"_source"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("unmarshal response error: %w", err)
	}

	if err := json.Unmarshal(response.Source, result); err != nil {
		return fmt.Errorf("unmarshal document error: %w", err)
	}

	return nil
}

// Delete 删除文档
func (d *Document) Delete(ctx context.Context, index string, docID string) error {
	req := esapi.DeleteRequest{
		Index:      index,
		DocumentID: docID,
		Refresh:    "true",
	}

	res, err := req.Do(ctx, d.client.Client)
	if err != nil {
		return fmt.Errorf("delete document error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("delete document failed: %s", res.String())
	}

	return nil
}
