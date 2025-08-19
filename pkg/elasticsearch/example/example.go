package main

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/pkg/elasticsearch"
	"github.com/olivere/elastic/v7"
	"log"
	"time"
)

func main() {
	// 创建客户端
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses:          []string{"http://43.143.78.234:9200"},
		Username:           "",
		Password:           "",
		Timeout:            10,
		InsecureSkipVerify: true, // 跳过TLS验证
	})
	if err != nil {
		log.Fatal("Error creating client:", err)
	}

	// 测试连接
	ctx := context.Background()
	exists, err := client.IndexExists(ctx, "study")
	fmt.Println(exists, err, "-----------")
	if err != nil {
		log.Fatal("Error checking index:", err)
	}

	fmt.Println("Index exists:", exists)

	// 创建索引（无mapping）
	err = client.CreateIndex(context.Background(), "my-index", nil)
	if err != nil {
		log.Fatal("Error creating index:", err)
	}

	// 创建索引（带mapping）
	mapping := map[string]interface{}{
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type": "text",
				},
				"content": map[string]interface{}{
					"type": "text",
				},
				"created": map[string]interface{}{
					"type": "date",
				},
			},
		},
	}

	err = client.CreateIndex(context.Background(), "my-index-with-mapping", mapping)
	if err != nil {
		log.Fatal("Error creating index with mapping:", err)
	}

	// 定义文档结构
	type Document struct {
		Title   string    `json:"title"`
		Content string    `json:"content"`
		Created time.Time `json:"created"`
	}

	// 插入文档
	doc := Document{
		Title:   "Hello Elasticsearch",
		Content: "This is a test document",
		Created: time.Now(),
	}

	// 自动分配ID
	err = client.IndexDocument(context.Background(), "my-index", "", doc)
	if err != nil {
		log.Fatal("Error indexing document:", err)
	}

	// 指定ID插入文档
	err = client.IndexDocument(context.Background(), "my-index", "doc-1", doc)
	if err != nil {
		log.Fatal("Error indexing document with ID:", err)
	}

	// 获取文档
	res, err := client.GetDocument(context.Background(), "my-index", "doc-1")
	if err != nil {
		log.Fatal("Error getting document:", err)
	}
	fmt.Printf("Document: %+v\n", res)

	// 更新文档
	updateDoc := map[string]interface{}{
		"content": "This is updated content",
	}

	err = client.UpdateDocument(context.Background(), "my-index", "doc-1", updateDoc)
	if err != nil {
		log.Fatal("Error updating document:", err)
	}

	// 简单搜索
	query := elastic.NewMatchQuery("title", "Hello")
	searchResult, err := client.Search(context.Background(), "my-index", query)
	if err != nil {
		log.Fatal("Error searching:", err)
	}

	fmt.Printf("Found %d documents\n", searchResult.Hits.TotalHits.Value)

	// 删除文档
	err = client.DeleteDocument(context.Background(), "my-index", "doc-1")
	if err != nil {
		log.Fatal("Error deleting document:", err)
	}

	// 删除索引
	err = client.DeleteIndex(context.Background(), "my-index")
	if err != nil {
		log.Fatal("Error deleting index:", err)
	}
}
