package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v7/esapi"
)

// Search 搜索操作接口
type Search struct {
	client *Client
}

// Search NewSearch 创建搜索操作实例
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

// SearchWithRawQuery 添加更灵活的搜索方法
func (s *Search) SearchWithRawQuery(ctx context.Context, index string, query []byte) (*SearchResult, error) {
	searchReq := esapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(query),
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

// Pagination 分页信息
type Pagination struct {
	Page     int `json:"page"`      // 页码，从1开始
	PageSize int `json:"page_size"` // 每页大小
}

// PaginatedSearchRequest 支持分页的搜索请求
type PaginatedSearchRequest struct {
	Query    interface{} `json:"query,omitempty"`
	Pagination Pagination `json:"pagination,omitempty"`
	Sort     interface{} `json:"sort,omitempty"`
	Source   interface{} `json:"_source,omitempty"`
}

// PaginatedResult 分页结果
type PaginatedResult struct {
	SearchResult
	Pagination PaginationResult `json:"pagination"`
}

// PaginationResult 分页结果信息
type PaginationResult struct {
	Page       int `json:"page"`        // 当前页码
	PageSize   int `json:"page_size"`   // 每页大小
	Total      int `json:"total"`       // 总记录数
	TotalPages int `json:"total_pages"` // 总页数
}

// SearchWithPagination 支持分页的搜索方法
func (s *Search) SearchWithPagination(ctx context.Context, index string, req PaginatedSearchRequest) (*PaginatedResult, error) {
	// 设置默认分页参数
	page := req.Pagination.Page
	pageSize := req.Pagination.PageSize
	
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	// 限制每页最大数量
	if pageSize > 10000 {
		pageSize = 10000
	}

	// 转换为标准搜索请求
	searchReq := SearchRequest{
		Query:  req.Query,
		From:   (page - 1) * pageSize,
		Size:   pageSize,
		Sort:   req.Sort,
		Source: req.Source,
	}

	result, err := s.Search(ctx, index, searchReq)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	total := result.Hits.Total.Value
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}

	return &PaginatedResult{
		SearchResult: *result,
		Pagination: PaginationResult{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// ScrollSearchResult Scroll搜索结果
type ScrollSearchResult struct {
	SearchResult
	ScrollID string `json:"_scroll_id,omitempty"`
}

// ScrollSearch 初始化Scroll搜索
func (s *Search) ScrollSearch(ctx context.Context, index string, req SearchRequest, scrollTime time.Duration) (*ScrollSearchResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal search request error: %w", err)
	}

	scrollReq := esapi.SearchRequest{
		Index:      []string{index},
		Body:       bytes.NewReader(body),
		Scroll:     scrollTime,
	}

	res, err := scrollReq.Do(ctx, s.client.Client)
	if err != nil {
		return nil, fmt.Errorf("scroll search error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("scroll search failed: %s", res.String())
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	var result ScrollSearchResult
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal scroll search result error: %w", err)
	}

	// 从响应中提取 scroll_id
	if scrollID := res.Header.Get("X-Elastic-Product"); scrollID != "" {
		result.ScrollID = scrollID
	}

	return &result, nil
}

// ScrollContinue 继续Scroll搜索
func (s *Search) ScrollContinue(ctx context.Context, scrollID string, scrollTime time.Duration) (*ScrollSearchResult, error) {
	scrollReq := esapi.ScrollRequest{
		ScrollID: scrollID,
		Scroll:   scrollTime,
	}

	res, err := scrollReq.Do(ctx, s.client.Client)
	if err != nil {
		return nil, fmt.Errorf("scroll continue error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("scroll continue failed: %s", res.String())
	}

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	var result ScrollSearchResult
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal scroll search result error: %w", err)
	}

	return &result, nil
}

// ScrollClear 清除Scroll上下文
func (s *Search) ScrollClear(ctx context.Context, scrollIDs []string) error {
	clearReq := esapi.ClearScrollRequest{
		Body: strings.NewReader(fmt.Sprintf(`{"scroll_id": ["%s"]}`, strings.Join(scrollIDs, `","`))),
	}

	res, err := clearReq.Do(ctx, s.client.Client)
	if err != nil {
		return fmt.Errorf("scroll clear error: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("scroll clear failed: %s", res.String())
	}

	return nil
}

// SearchRequestWithSearchAfter 带search_after的搜索请求
type SearchRequestWithSearchAfter struct {
	SearchRequest
	SearchAfter []interface{} `json:"search_after,omitempty"`
}

// SearchWithSearchAfter 使用search_after进行搜索
func (s *Search) SearchWithSearchAfter(ctx context.Context, index string, req SearchRequestWithSearchAfter) (*SearchResult, error) {
	// 确保设置了排序字段，这是使用 search_after 的前提条件
	if req.Sort == nil {
		return nil, fmt.Errorf("sort field is required when using search_after")
	}

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