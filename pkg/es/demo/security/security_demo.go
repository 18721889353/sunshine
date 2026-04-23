package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/es"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	fmt.Println("=== Elasticsearch 安全功能完整演示 ===")

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
	defer func() { _ = logger.Sync() }()

	// 使用选项模式创建客户端
	client, err := es.NewClient(
		es.WithConfig(config),
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

	// 其他操作使用配置的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// 1. 查看所有用户
	fmt.Println("\n=== 1. 查看所有用户 ===")
	getAllUsers(client, ctx)

	// 2. 角色管理演示
	fmt.Println("\n=== 2. 角色管理演示 ===")
	roleName := "demo_role"

	// 2.1 创建角色
	fmt.Printf("创建角色 %s...\n", roleName)
	createRoleDemo(client, ctx, roleName)

	// 2.2 获取角色信息
	fmt.Printf("获取角色 %s 信息...\n", roleName)
	getRoleDemo(client, ctx, roleName)

	// 2.3 更新角色
	fmt.Printf("更新角色 %s...\n", roleName)
	updateRoleDemo(client, ctx, roleName)

	// 2.4 再次获取角色验证更新
	fmt.Printf("再次获取角色 %s 信息以验证更新...\n", roleName)
	getRoleDemo(client, ctx, roleName)

	// 3. 用户管理演示
	fmt.Println("\n=== 3. 用户管理演示 ===")
	username := "demo_user"

	// 3.1 创建用户
	fmt.Printf("创建用户 %s...\n", username)
	createUserDemo(client, ctx, username, roleName)

	// 3.2 获取用户信息
	fmt.Printf("获取用户 %s 信息...\n", username)
	getUserDemo(client, ctx, username)

	// 3.3 更新用户
	fmt.Printf("更新用户 %s...\n", username)
	updateUserDemo(client, ctx, username, roleName)

	// 3.4 再次获取用户验证更新
	fmt.Printf("再次获取用户 %s 信息以验证更新...\n", username)
	getUserDemo(client, ctx, username)

	// 4. 权限验证演示
	fmt.Println("\n=== 4. 权限验证演示 ===")
	validatePermissions(client, ctx, username, roleName)

	// 5. 清理工作 - 删除用户和角色
	fmt.Println("\n=== 5. 清理工作 ===")
	cleanup(client, ctx, username, roleName)
}

// getAllUsers 查看所有用户
func getAllUsers(client *es.Client, ctx context.Context) {
	req := esapi.SecurityGetUserRequest{}
	res, err := req.Do(ctx, client.Client)
	if err != nil {
		log.Printf("获取用户列表失败: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		log.Printf("获取用户列表返回错误: %s", res.String())
		return
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("读取响应体失败: %v", err)
		return
	}

	fmt.Printf("用户列表: %s\n", string(body))
}

// createRoleDemo 创建角色演示
func createRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	role := es.Role{
		Cluster: []string{"monitor"},
		Applications: []es.ApplicationPrivileges{
			{
				Application: "myapp",
				Privileges:  []string{"read", "write"},
				Resources:   []string{"*"},
			},
		},
		Indices: []es.RoleIndicesPermissions{
			{
				Names:      []string{"*"},
				Privileges: []string{"read", "write"},
			},
		},
	}

	if err := client.CreateRole(ctx, roleName, role); err != nil {
		log.Printf("创建角色失败: %v", err)
		return
	}

	fmt.Printf("角色 %s 创建成功\n", roleName)
}

// getRoleDemo 获取角色信息演示
func getRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	role, err := client.GetRole(ctx, roleName)
	if err != nil {
		log.Printf("获取角色信息失败: %v", err)
		return
	}

	roleJSON, err := json.MarshalIndent(role, "", "  ")
	if err != nil {
		log.Printf("序列化角色信息失败: %v", err)
		return
	}

	fmt.Printf("角色 %s 信息:\n%s\n", roleName, string(roleJSON))
}

// updateRoleDemo 更新角色演示
func updateRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	role := es.Role{
		Cluster: []string{"monitor", "manage_index_templates"},
		Applications: []es.ApplicationPrivileges{
			{
				Application: "myapp",
				Privileges:  []string{"read", "write", "delete"},
				Resources:   []string{"*"},
			},
		},
		Indices: []es.RoleIndicesPermissions{
			{
				Names:      []string{"*"},
				Privileges: []string{"read", "write", "delete"},
			},
		},
	}

	if err := client.UpdateRole(ctx, roleName, role); err != nil {
		log.Printf("更新角色失败: %v", err)
		return
	}

	fmt.Printf("角色 %s 更新成功\n", roleName)
}

// createUserDemo 创建用户演示
func createUserDemo(client *es.Client, ctx context.Context, username, roleName string) {
	user := es.User{
		Password: "demo_password",
		Roles:    []string{roleName},
		FullName: "Demo User",
		Email:    "demo@example.com",
		Enabled:  true,
	}

	if err := client.CreateUser(ctx, username, user); err != nil {
		log.Printf("创建用户失败: %v", err)
		return
	}

	fmt.Printf("用户 %s 创建成功\n", username)
}

// getUserDemo 获取用户信息演示
func getUserDemo(client *es.Client, ctx context.Context, username string) {
	user, err := client.GetUser(ctx, username)
	if err != nil {
		log.Printf("获取用户信息失败: %v", err)
		return
	}

	userJSON, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		log.Printf("序列化用户信息失败: %v", err)
		return
	}

	fmt.Printf("用户 %s 信息:\n%s\n", username, string(userJSON))
}

// updateUserDemo 更新用户演示
func updateUserDemo(client *es.Client, ctx context.Context, username, roleName string) {
	user := es.User{
		Password: "new_demo_password",
		Roles:    []string{roleName},
		FullName: "Updated Demo User",
		Email:    "updated_demo@example.com",
		Enabled:  true,
	}

	if err := client.UpdateUser(ctx, username, user); err != nil {
		log.Printf("更新用户失败: %v", err)
		return
	}

	fmt.Printf("用户 %s 更新成功\n", username)
}

// validatePermissions 权限验证演示
func validatePermissions(client *es.Client, ctx context.Context, username, roleName string) {
	fmt.Printf("验证用户 %s 的权限...\n", username)

	// 尝试执行一个需要权限的操作
	req := esapi.ClusterHealthRequest{}
	res, err := req.Do(ctx, client.Client)
	if err != nil {
		log.Printf("权限验证失败: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		fmt.Printf("用户 %s 权限不足: %s\n", username, res.String())
	} else {
		fmt.Printf("用户 %s 有足够权限执行操作\n", username)
	}
}

// cleanup 清理工作
func cleanup(client *es.Client, ctx context.Context, username, roleName string) {
	// 删除用户
	if err := client.DeleteUser(ctx, username); err != nil {
		log.Printf("删除用户 %s 失败: %v", username, err)
	} else {
		fmt.Printf("用户 %s 删除成功\n", username)
	}

	// 删除角色
	if err := client.DeleteRole(ctx, roleName); err != nil {
		log.Printf("删除角色 %s 失败: %v", roleName, err)
	} else {
		fmt.Printf("角色 %s 删除成功\n", roleName)
	}
}
