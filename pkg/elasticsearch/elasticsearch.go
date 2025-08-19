package elasticsearch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/olivere/elastic/v7"
)

// Config represents the configuration for Elasticsearch client
type Config struct {
	// Addresses is a list of Elasticsearch nodes to use.
	Addresses []string `json:"addresses" yaml:"addresses"`

	// Username is the basic auth username for Elasticsearch.
	Username string `json:"username" yaml:"username"`

	// Password is the basic auth password for Elasticsearch.
	Password string `json:"password" yaml:"password"`

	// Sniff enables or disables sniffer (default: true).
	Sniff *bool `json:"sniff" yaml:"sniff"`

	// Healthcheck enables or disables healthchecks (default: true).
	Healthcheck *bool `json:"healthcheck" yaml:"healthcheck"`

	// Timeout in seconds for requests to Elasticsearch (default: 30 seconds).
	Timeout int `json:"timeout" yaml:"timeout"`

	// RetryOnFailure indicates whether to retry requests on failure.
	RetryOnFailure bool `json:"retryOnFailure" yaml:"retryOnFailure"`

	// MaxRetries is the maximum number of retries for a single request.
	MaxRetries int `json:"maxRetries" yaml:"maxRetries"`

	// EnableDebugging enables logging of requests and responses.
	EnableDebugging bool `json:"enableDebugging" yaml:"enableDebugging"`

	// EnableMetrics enables metrics collection.
	EnableMetrics bool `json:"enableMetrics" yaml:"enableMetrics"`

	// InsecureSkipVerify skips TLS verification if set to true.
	InsecureSkipVerify bool `json:"insecureSkipVerify" yaml:"insecureSkipVerify"`

	// DisableSniff disables the sniffer completely
	DisableSniff bool `json:"disableSniff" yaml:"disableSniff"`

	// DisableHealthcheck disables the healthchecks
	DisableHealthcheck bool `json:"disableHealthcheck" yaml:"disableHealthcheck"`

	// MaxIdleConnsPerHost controls the maximum idle (keep-alive) connections to keep per-host
	MaxIdleConnsPerHost int `json:"maxIdleConnsPerHost" yaml:"maxIdleConnsPerHost"`

	// MaxConnsPerHost limits the total number of connections per host
	MaxConnsPerHost int `json:"maxConnsPerHost" yaml:"maxConnsPerHost"`

	// IdleConnTimeout is the maximum amount of time an idle connection will remain in the connection pool
	IdleConnTimeout time.Duration `json:"idleConnTimeout" yaml:"idleConnTimeout"`
}

// Client wraps the official Elasticsearch client with additional functionality
type Client struct {
	*elastic.Client
	config Config
	mu     sync.RWMutex
}

// customRetrier implements a custom retrier with a maximum number of retries
type customRetrier struct {
	maxRetries int
	backoff    elastic.Backoff
}

// NewCustomRetrier creates a new custom retrier
func NewCustomRetrier(maxRetries int, backoff elastic.Backoff) *customRetrier {
	return &customRetrier{
		maxRetries: maxRetries,
		backoff:    backoff,
	}
}

// Retry implements the Retrier interface
func (r *customRetrier) Retry(ctx context.Context, retry int, req *http.Request, resp *http.Response, err error) (time.Duration, bool, error) {
	// Check if we reached the maximum number of retries
	if retry >= r.maxRetries {
		return 0, false, nil
	}

	// Check if the context has been cancelled
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}

	// Retry on network errors or 5xx status codes
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		wait, stop := r.backoff.Next(retry)
		return wait, !stop, nil
	}

	// Don't retry on other errors (e.g. 4xx status codes)
	return 0, false, nil
}

// NewClient creates a new Elasticsearch client with the provided configuration
func NewClient(cfg Config) (*Client, error) {
	// Set default timeout if not provided
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30
	}

	// Set default max retries if not provided
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	// Set default connection pool settings
	if cfg.MaxIdleConnsPerHost <= 0 {
		cfg.MaxIdleConnsPerHost = 10
	}

	if cfg.MaxConnsPerHost <= 0 {
		cfg.MaxConnsPerHost = 100
	}

	if cfg.IdleConnTimeout <= 0 {
		cfg.IdleConnTimeout = 90 * time.Second
	}

	// Create options for the Elasticsearch client
	options := []elastic.ClientOptionFunc{
		elastic.SetURL(cfg.Addresses...),
		elastic.SetBasicAuth(cfg.Username, cfg.Password),
		elastic.SetSniff(!cfg.DisableSniff),
		elastic.SetHealthcheck(!cfg.DisableHealthcheck),
		elastic.SetHttpClient(&http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify,
				},
				MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
				MaxConnsPerHost:     cfg.MaxConnsPerHost,
				IdleConnTimeout:     cfg.IdleConnTimeout,
			},
		}),
		elastic.SetSniff(false),
		// Use custom retrier instead of the deprecated SetMaxRetries
		elastic.SetRetrier(NewCustomRetrier(cfg.MaxRetries, elastic.NewExponentialBackoff(100*time.Millisecond, 30*time.Second))),
	}

	// Enable debugging if requested
	if cfg.EnableDebugging {
		options = append(options, elastic.SetTraceLog(&logger{}))
		options = append(options, elastic.SetInfoLog(&logger{}))
		options = append(options, elastic.SetErrorLog(&logger{}))
	}

	// Create the client
	client, err := elastic.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	_, _, err = client.Ping(cfg.Addresses[0]).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Elasticsearch at %s: %w", cfg.Addresses[0], err)
	}

	return &Client{
		Client: client,
		config: cfg,
	}, nil
}

// logger is a simple logger for debugging
type logger struct{}

func (l *logger) Printf(format string, v ...interface{}) {
	fmt.Printf("[ELASTIC] "+format+"\n", v...)
}

// GetConfig returns the client configuration
func (c *Client) GetConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// CreateIndex creates a new index with optional mapping
func (c *Client) CreateIndex(ctx context.Context, indexName string, mapping interface{}) error {
	var res *elastic.IndicesCreateResult
	var err error

	if mapping != nil {
		res, err = c.Client.CreateIndex(indexName).BodyJson(mapping).Do(ctx)
	} else {
		res, err = c.Client.CreateIndex(indexName).Do(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to create index %s: %w", indexName, err)
	}

	if !res.Acknowledged {
		return fmt.Errorf("failed to create index %s: not acknowledged", indexName)
	}

	return nil
}

// DeleteIndex deletes an index
func (c *Client) DeleteIndex(ctx context.Context, indexName string) error {
	res, err := c.Client.DeleteIndex(indexName).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete index %s: %w", indexName, err)
	}

	if !res.Acknowledged {
		return fmt.Errorf("failed to delete index %s: not acknowledged", indexName)
	}

	return nil
}

// IndexExists checks if an index exists
func (c *Client) IndexExists(ctx context.Context, indexName string) (bool, error) {
	exists, err := c.Client.IndexExists(indexName).Do(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if index %s exists: %w", indexName, err)
	}
	return exists, nil
}

// IndexDocument indexes a document with optional document ID
func (c *Client) IndexDocument(ctx context.Context, indexName string, documentID string, doc interface{}) error {
	var res *elastic.IndexResponse
	var err error

	if documentID != "" {
		res, err = c.Client.Index().
			Index(indexName).
			Id(documentID).
			BodyJson(doc).
			Do(ctx)
	} else {
		res, err = c.Client.Index().
			Index(indexName).
			BodyJson(doc).
			Do(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	if res.Result != "created" && res.Result != "updated" {
		return fmt.Errorf("failed to index document: unexpected result %s", res.Result)
	}

	return nil
}

// GetDocument retrieves a document by ID
func (c *Client) GetDocument(ctx context.Context, indexName string, documentID string) (*elastic.GetResult, error) {
	if documentID == "" {
		return nil, errors.New("document ID is required")
	}

	res, err := c.Client.Get().
		Index(indexName).
		Id(documentID).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get document %s: %w", documentID, err)
	}

	return res, nil
}

// UpdateDocument updates a document by ID
func (c *Client) UpdateDocument(ctx context.Context, indexName string, documentID string, doc interface{}) error {
	if documentID == "" {
		return errors.New("document ID is required")
	}

	res, err := c.Client.Update().
		Index(indexName).
		Id(documentID).
		Doc(doc).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to update document %s: %w", documentID, err)
	}

	if res.Result != "updated" {
		return fmt.Errorf("failed to update document %s: unexpected result %s", documentID, res.Result)
	}

	return nil
}

// DeleteDocument deletes a document by ID
func (c *Client) DeleteDocument(ctx context.Context, indexName string, documentID string) error {
	if documentID == "" {
		return errors.New("document ID is required")
	}

	res, err := c.Client.Delete().
		Index(indexName).
		Id(documentID).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete document %s: %w", documentID, err)
	}

	if res.Result != "deleted" {
		return fmt.Errorf("failed to delete document %s: unexpected result %s", documentID, res.Result)
	}

	return nil
}

// Search performs a search query
func (c *Client) Search(ctx context.Context, indexName string, query elastic.Query) (*elastic.SearchResult, error) {
	searchService := c.Client.Search()

	if indexName != "" {
		searchService = searchService.Index(indexName)
	}

	res, err := searchService.Query(query).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to perform search: %w", err)
	}

	return res, nil
}

// BulkIndex allows bulk indexing of documents
func (c *Client) BulkIndex(ctx context.Context, indexName string, docs []map[string]interface{}) (*elastic.BulkResponse, error) {
	bulkRequest := c.Client.Bulk()

	for _, doc := range docs {
		bulkRequest = bulkRequest.Add(elastic.NewBulkIndexRequest().Index(indexName).Doc(doc))
	}

	res, err := bulkRequest.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to perform bulk index: %w", err)
	}

	if res.Errors {
		return res, fmt.Errorf("bulk index completed with errors")
	}

	return res, nil
}
