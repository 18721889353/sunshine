package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// Search 搜索操作接口
type Search struct {
	client *Client
}

// NewSearch 创建搜索操作实例
func (c *Client) Search() *Search {
	return &Search{client: c}
}

// SearchRequest 搜索请求
type SearchRequest struct {
	Query  interface{} `json:"query,omitempty"`
	Size   int         `json:"size,omitempty"`
	From   int         `json:"from,omitempty"`
	Sort   interface{} `json:"sort,omitempty"`
	Source interface{} `json:"_source,omitempty"`
}

// SearchResult 搜索结果
type SearchResult struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	Hits     struct {
		Total struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		Hits []struct {
			Index  string          `json:"_index"`
			ID     string          `json:"_id"`
			Score  float64         `json:"_score"`
			Source json.RawMessage `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// Search 执行搜索
func (s *Search) Search(ctx context.Context, index string, req SearchRequest) (*SearchResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal search request error: %w", err)
	}

	searchReq := esapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(body),
	}

	res, err := searchReq.Do(ctx, s.client.Client)
	if err != nil {
		return nil, fmt.Errorf("search error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search failed: %s", res.String())
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	var result SearchResult
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal search result error: %w", err)
	}

	return &result, nil
}
