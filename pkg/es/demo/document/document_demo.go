package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap/zapcore"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/es"
	"go.uber.org/zap"
)

// User 用户结构体，用于映射Elasticsearch中的文档
type User struct {
	Name      string    `json:"name"`       // 用户名
	Age       int       `json:"age"`        // 年龄
	Email     string    `json:"email"`      // 邮箱
	CreatedAt time.Time `json:"created_at"` // 创建时间
}

func main() {
	fmt.Println("=== Elasticsearch 文档操作完整演示 ===")

	// 初始化客户端，使用选项模式配置
	config := es.GetDefaultConfig()
	config.Addresses = []string{"http://43.143.78.234:9200"} // Elasticsearch服务地址
	config.Username = "elastic"                              // 用户名
	config.Password = "jianguo123"                           // 密码

	// 自定义连接池参数
	config.MaxIdleConns = 30                    // 最大空闲连接数
	config.MaxIdleConnsPerHost = 10             // 每个主机最大空闲连接数
	config.MaxConnsPerHost = 50                 // 每个主机最大连接数
	config.IdleConnTimeout = 120 * time.Second  // 空闲连接超时时间
	config.ConnectionTimeout = 10 * time.Second // 连接超时时间

	// 自定义重试参数
	config.MaxRetries = 5                        // 最大重试次数
	config.RetryBackoff = 200 * time.Millisecond // 重试间隔

	// 创建只记录告警级别及以上日志的logger
	// 设置日志级别为 WarnLevel，只记录警告和错误级别日志
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	logger, err := cfg.Build()
	if err != nil {
		log.Fatal("Failed to create logger:", err)
	}
	defer logger.Sync()
	client, err := es.NewClient(
		es.WithConfig(config),
		es.WithLogger(logger),
	)
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}

	// 连接检查使用较短的超时
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()

	if err := client.Ping(connectCtx); err != nil {
		log.Fatal("ES connection failed:", err)
	}

	// 检查集群健康状态
	health, err := client.HealthCheck(connectCtx)
	if err != nil {
		log.Fatal("Failed to check cluster health:", err)
	}
	fmt.Printf("Cluster health status: %s\n", health)

	info, err := client.Info(connectCtx)
	if err != nil {
		log.Fatal("Failed to get ES info:", err)
	}

	fmt.Printf("Connected to Elasticsearch cluster: %s (version: %s)\n",
		info["cluster_name"], info["version"].(map[string]interface{})["number"])

	// 其他操作使用配置的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// 1. 集群信息演示
	fmt.Println("\n=== 1. 集群信息演示 ===")
	clusterInfoDemo(client, ctx)

	// 确保索引存在，并定义合适的映射
	indexName := "users"
	if exists, _ := client.Document().IndexExists(ctx, indexName); !exists {
		// 创建索引并定义映射
		indexMapping := map[string]interface{}{
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type": "text",
						"fields": map[string]interface{}{
							"keyword": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
					"age": map[string]interface{}{
						"type": "integer",
					},
					"email": map[string]interface{}{
						"type": "keyword",
					},
					"created_at": map[string]interface{}{
						"type": "date",
					},
				},
			},
		}

		if err := client.Document().CreateIndex(ctx, indexName, indexMapping); err != nil {
			log.Printf("创建索引失败: %v", err)
		} else {
			fmt.Printf("索引 %s 创建成功\n", indexName)
		}
	}

	// 2. 单文档操作演示
	fmt.Println("\n=== 2. 单文档操作演示 ===")
	docID := "user_1"
	singleDocumentDemo(client, ctx, indexName, docID)

	// 3. 批量操作演示
	fmt.Println("\n=== 3. 批量操作演示 ===")
	bulkOperationsDemo(client, ctx, indexName)

	// 4. 搜索操作演示
	fmt.Println("\n=== 4. 搜索操作演示 ===")
	searchOperationsDemo(client, ctx, indexName)

	fmt.Println("\n=== 所有文档操作功能演示完成 ===")

	// 5. 清理操作
	fmt.Println("\n=== 5. 清理操作 ===")
	cleanupDemo(client, ctx, indexName)
}

// 集群信息演示
func clusterInfoDemo(client *es.Client, ctx context.Context) {
	// 获取集群健康状态
	health, err := client.HealthCheck(ctx)
	if err != nil {
		log.Printf("获取集群健康状态失败: %v", err)
	} else {
		fmt.Printf("集群健康状态: %s\n", health)
	}

	// 获取集群信息
	info, err := client.Info(ctx)
	if err != nil {
		log.Printf("获取集群信息失败: %v", err)
	} else {
		fmt.Printf("集群名称: %s, 版本: %s\n",
			info["cluster_name"],
			info["version"].(map[string]interface{})["number"])
	}
}

// 单文档操作演示
func singleDocumentDemo(client *es.Client, ctx context.Context, index, docID string) {
	// 2.1 索引单个文档
	fmt.Printf("索引单个文档 %s...\n", docID)
	indexDocumentDemo(client, ctx, index, docID)

	// 2.2 获取文档
	fmt.Printf("获取文档 %s...\n", docID)
	getDocumentDemo(client, ctx, index, docID)

	// 2.3 更新文档
	fmt.Printf("更新文档 %s...\n", docID)
	updateDocumentDemo(client, ctx, index, docID)

	// 2.4 再次获取文档验证更新
	fmt.Printf("再次获取文档 %s 验证更新...\n", docID)
	getDocumentDemo(client, ctx, index, docID)

	// 2.5 删除文档
	fmt.Printf("删除文档 %s...\n", docID)
	deleteDocumentDemo(client, ctx, index, docID)

	// 2.6 验证文档已删除
	fmt.Printf("验证文档 %s 已删除...\n", docID)
	getDocumentDemo(client, ctx, index, docID)
}

// 索引文档演示
func indexDocumentDemo(client *es.Client, ctx context.Context, index, docID string) {
	user := User{
		Name:      "张三",
		Age:       25,
		Email:     "zhangsan@example.com",
		CreatedAt: time.Now(),
	}

	if err := client.Document().Index(ctx, index, user, docID); err != nil {
		log.Printf("索引文档失败: %v", err)
	} else {
		fmt.Printf("文档 %s 索引成功\n", docID)
	}
}

// 获取文档演示
func getDocumentDemo(client *es.Client, ctx context.Context, index, docID string) {
	var user User
	if err := client.Document().Get(ctx, index, docID, &user); err != nil {
		if err.Error() == "document not found" {
			fmt.Printf("文档 %s 不存在\n", docID)
		} else {
			log.Printf("获取文档失败: %v", err)
		}
		return
	}
	fmt.Printf("获取到的文档 %s: %+v\n", docID, user)
}

// 更新文档演示
func updateDocumentDemo(client *es.Client, ctx context.Context, index, docID string) {
	updateData := map[string]interface{}{
		"age": 26,
	}
	if err := client.Document().Update(ctx, index, docID, updateData); err != nil {
		log.Printf("更新文档失败: %v", err)
	} else {
		fmt.Printf("文档 %s 更新成功\n", docID)
	}
}

// 删除文档演示
func deleteDocumentDemo(client *es.Client, ctx context.Context, index, docID string) {
	if err := client.Document().Delete(ctx, index, docID); err != nil {
		log.Printf("删除文档失败: %v", err)
	} else {
		fmt.Printf("文档 %s 删除成功\n", docID)
	}
}

// 批量操作演示
func bulkOperationsDemo(client *es.Client, ctx context.Context, index string) {
	// 3.1 批量索引文档
	fmt.Println("批量索引文档...")
	bulkIndexDemo(client, ctx, index)

	// 3.2 批量创建文档
	fmt.Println("批量创建文档...")
	bulkCreateDemo(client, ctx, index)

	// 3.3 批量更新文档
	fmt.Println("批量更新文档...")
	bulkUpdateDemo(client, ctx, index)

	// 3.4 批量删除文档
	fmt.Println("批量删除文档...")
	bulkDeleteDemo(client, ctx, index)

	// 3.5 直接使用Bulk接口进行混合操作
	fmt.Println("混合批量操作...")
	mixedBulkDemo(client, ctx, index)
}

// 批量索引文档演示
func bulkIndexDemo(client *es.Client, ctx context.Context, index string) {
	// 创建带指定ID的文档
	newUsersWithID := make([]map[string]interface{}, 0)
	for i := 0; i < 2; i++ {
		newUsersWithID = append(newUsersWithID, map[string]interface{}{
			"_id":        fmt.Sprintf("bulk_create_%d", i),
			"name":       fmt.Sprintf("新用户%d", i),
			"age":        60 + i,
			"email":      fmt.Sprintf("newuser%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	fmt.Println("准备创建带ID的文档...")
	if err := client.Document().BulkIndex(ctx, index, newUsersWithID); err != nil {
		log.Printf("批量创建带ID的文档失败: %v", err)
		return
	} else {
		fmt.Println("批量创建带ID的文档完成")
	}

	// 创建不带指定ID的文档（让ES自动生成ID）
	newUsersWithoutID := make([]map[string]interface{}, 0)
	for i := 0; i < 2; i++ {
		newUsersWithoutID = append(newUsersWithoutID, map[string]interface{}{
			"name":       fmt.Sprintf("自动生成ID用户%d", i),
			"age":        25 + i,
			"email":      fmt.Sprintf("auto_id_user%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	fmt.Println("准备创建不带ID的文档...")
	if err := client.Document().BulkIndex(ctx, index, newUsersWithoutID); err != nil {
		log.Printf("批量创建不带ID的文档失败: %v", err)
		return
	} else {
		fmt.Println("批量创建不带ID的文档完成")
	}

	// 等待一下确保文档被索引
	time.Sleep(1 * time.Second)

	// 验证带指定ID的数据是否创建成功 - 使用terms精确匹配查询
	fmt.Println("验证批量创建带指定ID的文档...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_phrase": map[string]interface{}{
				"name": "新用户",
			},
		},
		Size: 10,
	}
	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个带指定ID的文档\n", searchResult.Hits.Total.Value)
	for _, hit := range searchResult.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}

	// 验证自动生成ID的文档 - 使用match查询匹配名称，并且排除已知的指定ID文档
	fmt.Println("验证自动生成ID的文档...")
	searchReq2 := es.SearchRequest{
		Query: map[string]interface{}{
			"match_phrase": map[string]interface{}{
				"name": "自动生成ID用户",
			},
		},
		Size: 10,
	}

	searchResult2, err := client.Search().Search(ctx, index, searchReq2)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个自动生成ID的文档\n", searchResult2.Hits.Total.Value)
	for _, hit := range searchResult2.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}

	// 验证所有文档
	fmt.Println("验证所有文档...")
	searchReq3 := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Size: 20,
	}

	searchResult3, err := client.Search().Search(ctx, index, searchReq3)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("索引中总共有 %d 个文档\n", searchResult3.Hits.Total.Value)
	for i, hit := range searchResult3.Hits.Hits {
		fmt.Printf("文档 %d ID: %s\n", i+1, hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}

}

// 批量创建文档演示
func bulkCreateDemo(client *es.Client, ctx context.Context, index string) {
	// 创建带指定ID的文档
	newUsersWithID := make([]map[string]interface{}, 0)
	for i := 0; i < 2; i++ {
		newUsersWithID = append(newUsersWithID, map[string]interface{}{
			"_id":        fmt.Sprintf("bulk_create_%d", i),
			"name":       fmt.Sprintf("新用户%d", i),
			"age":        20 + i,
			"email":      fmt.Sprintf("newuser%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	fmt.Println("准备创建带ID的文档...")
	if err := client.Document().BulkCreate(ctx, index, newUsersWithID); err != nil {
		log.Printf("批量创建带ID的文档失败: %v", err)
		return
	} else {
		fmt.Println("批量创建带ID的文档完成")
	}

	// 创建不带指定ID的文档（让ES自动生成ID）
	newUsersWithoutID := make([]map[string]interface{}, 0)
	for i := 0; i < 2; i++ {
		newUsersWithoutID = append(newUsersWithoutID, map[string]interface{}{
			"name":       fmt.Sprintf("自动生成ID用户%d", i),
			"age":        25 + i,
			"email":      fmt.Sprintf("auto_id_user%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	fmt.Println("准备创建不带ID的文档...")
	if err := client.Document().BulkCreate(ctx, index, newUsersWithoutID); err != nil {
		log.Printf("批量创建不带ID的文档失败: %v", err)
		return
	} else {
		fmt.Println("批量创建不带ID的文档完成")
	}

	// 等待一下确保文档被索引
	time.Sleep(1 * time.Second)

	// 验证带指定ID的数据是否创建成功 - 使用terms精确匹配查询
	fmt.Println("验证批量创建带指定ID的文档...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_phrase": map[string]interface{}{
				"name": "新用户",
			},
		},
		Size: 10,
	}

	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个带指定ID的文档\n", searchResult.Hits.Total.Value)
	for _, hit := range searchResult.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}

	// 验证自动生成ID的文档 - 使用match查询匹配名称，并且排除已知的指定ID文档
	fmt.Println("验证自动生成ID的文档...")
	searchReq2 := es.SearchRequest{
		Query: map[string]interface{}{
			"match_phrase": map[string]interface{}{
				"name": "自动生成ID用户",
			},
		},
		Size: 10,
	}

	searchResult2, err := client.Search().Search(ctx, index, searchReq2)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个自动生成ID的文档\n", searchResult2.Hits.Total.Value)
	for _, hit := range searchResult2.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}

	// 验证所有文档
	fmt.Println("验证所有文档...")
	searchReq3 := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Size: 20,
	}

	searchResult3, err := client.Search().Search(ctx, index, searchReq3)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("索引中总共有 %d 个文档\n", searchResult3.Hits.Total.Value)
	for i, hit := range searchResult3.Hits.Hits {
		fmt.Printf("文档 %d ID: %s\n", i+1, hit.ID)
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			log.Printf("解析文档失败: %v", err)
			continue
		}
		fmt.Printf("  内容: %+v\n", doc)
	}
}

// 批量更新文档演示
func bulkUpdateDemo(client *es.Client, ctx context.Context, index string) {
	// 准备更新数据，每个文档都需要包含_id字段
	updates := []map[string]interface{}{
		{"_id": "bulk_create_0", "age": 1},
		{"_id": "bulk_create_1", "age": 100},
	}

	if err := client.Document().BulkUpdate(ctx, index, updates); err != nil {
		log.Printf("批量更新失败: %v", err)
	} else {
		fmt.Println("批量更新完成")
	}

	// 等待一下确保更新完成
	time.Sleep(1 * time.Second)

	// 验证数据是否更新成功 - 查询特定ID的文档
	fmt.Println("验证批量更新的文档...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"terms": map[string]interface{}{
				"_id": []string{"bulk_create_0", "bulk_create_1"},
			},
		},
		Size: 10,
	}

	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个匹配的文档\n", searchResult.Hits.Total.Value)
	for _, hit := range searchResult.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}
}

// 批量删除文档演示
func bulkDeleteDemo(client *es.Client, ctx context.Context, index string) {
	idsToDelete := []string{"bulk_create_0", "bulk_create_1"} // 修正要删除的文档ID
	if err := client.Document().BulkDelete(ctx, index, idsToDelete); err != nil {
		log.Printf("批量删除失败: %v", err)
	} else {
		fmt.Println("批量删除完成")
	}

	// 等待一下确保删除完成
	time.Sleep(1 * time.Second)

	// 验证数据是否删除成功 - 查询特定ID的文档
	fmt.Println("验证批量删除的文档...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"terms": map[string]interface{}{
				"_id": idsToDelete,
			},
		},
		Size: 10,
	}

	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个匹配的文档 (应为0)\n", searchResult.Hits.Total.Value)
	for _, hit := range searchResult.Hits.Hits {
		fmt.Printf("文档ID: %s (该文档应未被删除)\n", hit.ID)
	}
}

// 混合批量操作演示
func mixedBulkDemo(client *es.Client, ctx context.Context, index string) {
	operations := []es.BulkOperation{
		{Index: index, ID: "mixed_1", Action: "index", Payload: map[string]interface{}{"name": "混合操作用户1", "age": 40}},
		{Index: index, ID: "mixed_2", Action: "create", Payload: map[string]interface{}{"name": "混合操作用户2", "age": 41}},
		{Index: index, ID: "mixed_2", Action: "update", Payload: map[string]interface{}{"doc": map[string]interface{}{"age": 55}}},
		{Index: index, ID: "mixed_1", Action: "delete"},
	}

	if err := client.Bulk().BulkExecute(ctx, operations); err != nil {
		log.Printf("混合批量操作失败: %v", err)
	} else {
		fmt.Println("混合批量操作完成")
	}

	// 等待一下确保操作完成
	time.Sleep(1 * time.Second)

	// 验证混合操作结果 - 查询特定ID的文档
	fmt.Println("验证混合操作结果...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"terms": map[string]interface{}{
				"_id": []string{"mixed_1", "mixed_2"},
			},
		},
		Size: 10,
	}

	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个匹配的文档\n", searchResult.Hits.Total.Value)
	for _, hit := range searchResult.Hits.Hits {
		fmt.Printf("文档ID: %s\n", hit.ID)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}

	// 验证删除操作
	fmt.Println("验证删除操作...")
	searchReq2 := es.SearchRequest{
		Query: map[string]interface{}{
			"terms": map[string]interface{}{
				"_id": []string{"mixed_1"},
			},
		},
		Size: 10,
	}

	searchResult2, err := client.Search().Search(ctx, index, searchReq2)
	if err != nil {
		log.Printf("验证搜索失败: %v", err)
		return
	}

	if searchResult2.Hits.Total.Value == 0 {
		fmt.Println("文档 mixed_1 已成功删除")
	} else {
		fmt.Printf("文档 mixed_1 仍然存在\n")
		for _, hit := range searchResult2.Hits.Hits {
			fmt.Printf("文档ID: %s\n", hit.ID)
		}
	}
}

// 搜索操作演示
func searchOperationsDemo(client *es.Client, ctx context.Context, index string) {
	// 4.1 基本搜索
	fmt.Println("基本搜索...")
	basicSearchDemo(client, ctx, index)

	// 4.2 使用原始查询搜索
	fmt.Println("使用原始查询搜索...")
	rawQuerySearchDemo(client, ctx, index)

	// 4.3 分页查询示例
	fmt.Println("分页查询示例...")
	paginatedSearchDemo(client, ctx, index)

	// 4.4 Scroll API 示例
	fmt.Println("Scroll API 示例...")
	scrollSearchDemo(client, ctx, index)

	// 4.5 Search After 示例
	fmt.Println("Search After 示例...")
	searchAfterDemo(client, ctx, index)
}

// 基本搜索演示
func basicSearchDemo(client *es.Client, ctx context.Context, index string) {
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"range": map[string]interface{}{
				"age": map[string]interface{}{
					"gte": 20,
					"lte": 50,
				},
			},
		},
		Size: 20,
	}

	searchResult, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个文档\n", searchResult.Hits.Total.Value)

	// 打印搜索结果
	for _, hit := range searchResult.Hits.Hits {
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("文档ID: %s, 分数: %.2f, 用户: %+v\n", hit.ID, hit.Score, user)
	}
}

// 原始查询搜索演示
func rawQuerySearchDemo(client *es.Client, ctx context.Context, index string) {
	rawQuery := []byte(`{
		"query": {
			"match": {
				"name": "用户"
			}
		},
		"size": 10
	}`)

	rawSearchResult, err := client.Search().SearchWithRawQuery(ctx, index, rawQuery)
	if err != nil {
		log.Printf("原始查询搜索失败: %v", err)
		return
	}

	fmt.Printf("使用原始查询找到 %d 个文档\n", rawSearchResult.Hits.Total.Value)
}

// 分页查询演示
func paginatedSearchDemo(client *es.Client, ctx context.Context, index string) {
	paginatedReq := es.PaginatedSearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Pagination: es.Pagination{
			Page:     1,
			PageSize: 3,
		},
		Sort: map[string]interface{}{
			"age": map[string]interface{}{
				"order": "asc",
			},
		},
	}

	paginatedResult, err := client.Search().SearchWithPagination(ctx, index, paginatedReq)
	if err != nil {
		log.Printf("分页搜索失败: %v", err)
		return
	}

	fmt.Printf("第%d页，共%d页，总共%d条记录\n",
		paginatedResult.Pagination.Page,
		paginatedResult.Pagination.TotalPages,
		paginatedResult.Pagination.Total)
}

// Scroll搜索演示
func scrollSearchDemo(client *es.Client, ctx context.Context, index string) {
	scrollReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Size: 5,
		Sort: []map[string]interface{}{
			{"age": map[string]interface{}{"order": "asc"}},
			{"_id": map[string]interface{}{"order": "asc"}},
		},
	}

	// 初始化 Scroll 搜索
	scrollResult, err := client.Search().ScrollSearch(ctx, index, scrollReq, 1*time.Minute)
	if err != nil {
		log.Printf("Scroll搜索失败: %v", err)
		return
	}

	fmt.Printf("通过 Scroll 获取到 %d 个文档\n", len(scrollResult.Hits.Hits))

	// 继续 Scroll 搜索
	if scrollResult.ScrollID != "" {
		nextScrollResult, err := client.Search().ScrollContinue(ctx, scrollResult.ScrollID, 1*time.Minute)
		if err != nil {
			log.Printf("继续 Scroll 搜索失败: %v", err)
		} else {
			fmt.Printf("通过继续 Scroll 获取到 %d 个文档\n", len(nextScrollResult.Hits.Hits))
		}

		// 清除 Scroll 上下文
		err = client.Search().ScrollClear(ctx, []string{scrollResult.ScrollID})
		if err != nil {
			log.Printf("警告: 清除 Scroll 上下文失败: %v", err)
		}
	}
}

// Search After 演示
func searchAfterDemo(client *es.Client, ctx context.Context, index string) {
	searchAfterReq := es.SearchRequestWithSearchAfter{
		SearchRequest: es.SearchRequest{
			Query: map[string]interface{}{
				"match_all": map[string]interface{}{},
			},
			Size: 3,
			Sort: []map[string]interface{}{
				{"age": map[string]interface{}{"order": "asc"}},
				{"_id": map[string]interface{}{"order": "asc"}},
			},
		},
	}

	searchAfterResult, err := client.Search().SearchWithSearchAfter(ctx, index, searchAfterReq)
	if err != nil {
		log.Printf("Search After 失败: %v", err)
		return
	}

	fmt.Printf("通过 Search After 获取到 %d 个文档\n", len(searchAfterResult.Hits.Hits))
}

// 清理函数，用于删除演示过程中创建的索引
func cleanupDemo(client *es.Client, ctx context.Context, index string) {
	fmt.Printf("开始清理索引 %s...\n", index)

	// 检查索引是否存在
	exists, err := client.Document().IndexExists(ctx, index)
	if err != nil {
		log.Printf("检查索引是否存在时出错: %v", err)
		return
	}

	if !exists {
		fmt.Printf("索引 %s 不存在，无需清理\n", index)
		return
	}

	// 删除索引
	if err := client.Document().DeleteIndex(ctx, index); err != nil {
		log.Printf("删除索引 %s 失败: %v", index, err)
	} else {
		fmt.Printf("索引 %s 删除成功\n", index)
	}
}

// 搜索文档演示
func searchDemo(client *es.Client, ctx context.Context, index string) {
	fmt.Println("搜索文档演示...")

	// 1. 基本搜索 - 匹配所有文档
	fmt.Println("1. 搜索所有文档...")
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Size: 10,
	}

	result, err := client.Search().Search(ctx, index, searchReq)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("总共找到 %d 个文档\n", result.Hits.Total.Value)
	for _, hit := range result.Hits.Hits {
		fmt.Printf("文档ID: %s, 分数: %f\n", hit.ID, hit.Score)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}

	// 2. 精确匹配搜索 - 匹配名称包含"新用户"的文档
	fmt.Println("\n2. 搜索名称包含\"新用户\"的文档...")
	searchReq2 := es.SearchRequest{
		Query: map[string]interface{}{
			"match": map[string]interface{}{
				"name": "新用户",
			},
		},
		Size: 10,
	}

	result2, err := client.Search().Search(ctx, index, searchReq2)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个名称包含\"新用户\"的文档\n", result2.Hits.Total.Value)
	for _, hit := range result2.Hits.Hits {
		fmt.Printf("文档ID: %s, 分数: %f\n", hit.ID, hit.Score)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}

	// 3. 布尔查询 - 必须匹配包含"新用户"的文档
	fmt.Println("\n3. 使用布尔查询必须匹配包含\"新用户\"的文档...")
	searchReq3 := es.SearchRequest{
		Query: map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{
					map[string]interface{}{
						"match": map[string]interface{}{
							"name": "新用户",
						},
					},
				},
			},
		},
		Size: 10,
	}

	result3, err := client.Search().Search(ctx, index, searchReq3)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("通过布尔查询找到 %d 个名称包含\"新用户\"的文档\n", result3.Hits.Total.Value)
	for _, hit := range result3.Hits.Hits {
		fmt.Printf("文档ID: %s, 分数: %f\n", hit.ID, hit.Score)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}

	// 4. 范围查询 - 年龄在30到40之间的用户
	fmt.Println("\n4. 搜索年龄在30到40之间的用户...")
	searchReq4 := es.SearchRequest{
		Query: map[string]interface{}{
			"range": map[string]interface{}{
				"age": map[string]interface{}{
					"gte": 30,
					"lte": 40,
				},
			},
		},
		Size: 10,
	}

	result4, err := client.Search().Search(ctx, index, searchReq4)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个年龄在30到40之间的用户\n", result4.Hits.Total.Value)
	for _, hit := range result4.Hits.Hits {
		fmt.Printf("文档ID: %s, 分数: %f\n", hit.ID, hit.Score)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}

	// 5. 复合查询 - 必须匹配"新用户"并且年龄大于等于30
	fmt.Println("\n5. 复合查询 - 必须匹配\"新用户\"并且年龄大于等于30...")
	searchReq5 := es.SearchRequest{
		Query: map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{
					map[string]interface{}{
						"match": map[string]interface{}{
							"name": "新用户",
						},
					},
					map[string]interface{}{
						"range": map[string]interface{}{
							"age": map[string]interface{}{
								"gte": 30,
							},
						},
					},
				},
			},
		},
		Size: 10,
	}

	result5, err := client.Search().Search(ctx, index, searchReq5)
	if err != nil {
		log.Printf("搜索失败: %v", err)
		return
	}

	fmt.Printf("找到 %d 个匹配条件的用户\n", result5.Hits.Total.Value)
	for _, hit := range result5.Hits.Hits {
		fmt.Printf("文档ID: %s, 分数: %f\n", hit.ID, hit.Score)
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("解析文档 %s 失败: %v", hit.ID, err)
			continue
		}
		fmt.Printf("  用户信息: %+v\n", user)
	}
}
