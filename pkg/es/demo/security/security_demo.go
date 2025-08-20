package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/es"
	"github.com/elastic/go-elasticsearch/v7/esapi"
)

func main() {
	fmt.Println("=== Elasticsearch 安全功能完整演示 ===")

	// 初始化客户端，使用完全自定义的连接池配置
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

	// 验证配置
	if err := config.Validate(); err != nil {
		log.Fatal("Invalid configuration:", err)
	}

	client, err := es.NewClient(config)
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

	// 3.5 修改用户密码
	fmt.Printf("修改用户 %s 密码...\n", username)
	changeUserPasswordDemo(client, ctx, username)

	// 4. 清理操作
	fmt.Println("\n=== 4. 清理操作 ===")

	// 4.1 删除用户
	fmt.Printf("删除用户 %s...\n", username)
	deleteUserDemo(client, ctx, username)

	// 4.2 删除角色
	fmt.Printf("删除角色 %s...\n", roleName)
	deleteRoleDemo(client, ctx, roleName)

	fmt.Println("\n=== 安全功能完整演示完成 ===")
}

// 获取所有用户信息
func getAllUsers(client *es.Client, ctx context.Context) {
	req := esapi.SecurityGetUserRequest{}

	res, err := req.Do(ctx, client.Client)
	if err != nil {
		log.Printf("获取用户信息失败: %s", err)
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		log.Printf("获取用户信息错误响应: %s", res.String())
		return
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("读取响应体失败: %s", err)
		return
	}

	fmt.Printf("所有用户信息: %s\n", string(body))
}

// 创建角色演示
func createRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	role := es.Role{
		Cluster: []string{"monitor"},
		Indices: []es.RoleIndicesPermissions{
			{
				Names:      []string{"logs-*"},
				Privileges: []string{"read", "view_index_metadata"},
			},
		},
		Applications: []es.ApplicationPrivileges{
			{
				Application: "myapp",
				Privileges:  []string{"read", "write"},
				Resources:   []string{"*"},
			},
		},
		Metadata: map[string]interface{}{
			"creator": "security_demo",
			"created": time.Now().Format(time.RFC3339),
		},
	}

	if err := client.CreateRole(ctx, roleName, role); err != nil {
		log.Printf("创建角色失败: %v", err)
	} else {
		fmt.Printf("角色 %s 创建成功\n", roleName)
	}
}

// 获取角色演示
func getRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	role, err := client.GetRole(ctx, roleName)
	if err != nil {
		log.Printf("获取角色失败: %v", err)
		return
	}
	fmt.Printf("获取到的角色信息: %+v\n", role)
}

// 更新角色演示
func updateRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	// 先获取现有角色信息
	role, err := client.GetRole(ctx, roleName)
	if err != nil {
		log.Printf("获取角色失败，无法更新: %v", err)
		return
	}

	updatedRole := es.Role{
		Cluster: []string{"monitor", "read_ilm"},
		Indices: []es.RoleIndicesPermissions{
			{
				Names:      []string{"logs-*", "metrics-*"},
				Privileges: []string{"read", "view_index_metadata", "manage"},
			},
		},
		Applications: []es.ApplicationPrivileges{
			{
				Application: "myapp",
				Privileges:  []string{"read", "write", "admin"},
				Resources:   []string{"*"},
			},
		},
		Metadata: map[string]interface{}{
			"creator":     "security_demo",
			"created":     role.Metadata["created"],
			"last_update": time.Now().Format(time.RFC3339),
		},
	}

	if err := client.UpdateRole(ctx, roleName, updatedRole); err != nil {
		log.Printf("更新角色失败: %v", err)
	} else {
		fmt.Printf("角色 %s 更新成功\n", roleName)
	}
}

// 删除角色演示
func deleteRoleDemo(client *es.Client, ctx context.Context, roleName string) {
	if err := client.DeleteRole(ctx, roleName); err != nil {
		log.Printf("删除角色失败: %v", err)
	} else {
		fmt.Printf("角色 %s 删除成功\n", roleName)
	}
}

// 创建用户演示
func createUserDemo(client *es.Client, ctx context.Context, username, roleName string) {
	user := es.User{
		Username: username,
		FullName: "Demo User",
		Email:    "demo@example.com",
		Roles:    []string{roleName},
		Password: "DemoPass123!",
		Metadata: map[string]interface{}{
			"creator": "security_demo",
			"created": time.Now().Format(time.RFC3339),
		},
		Enabled: true,
	}

	if err := client.CreateUser(ctx, username, user); err != nil {
		log.Printf("创建用户失败: %v", err)
	} else {
		fmt.Printf("用户 %s 创建成功\n", username)
	}
}

// 获取用户演示
func getUserDemo(client *es.Client, ctx context.Context, username string) {
	user, err := client.GetUser(ctx, username)
	if err != nil {
		log.Printf("获取用户失败: %v", err)
		return
	}
	fmt.Printf("获取到的用户信息: %+v\n", user)
}

// 更新用户演示
func updateUserDemo(client *es.Client, ctx context.Context, username, roleName string) {
	// 先获取现有用户信息
	user, err := client.GetUser(ctx, username)
	if err != nil {
		log.Printf("获取用户失败，无法更新: %v", err)
		return
	}

	updatedUser := es.User{
		Username: username,
		FullName: "Updated Demo User",
		Email:    "updated_demo@example.com",
		Roles:    []string{roleName, "superuser"},
		Metadata: map[string]interface{}{
			"creator":     "security_demo",
			"created":     user.Metadata["created"],
			"last_update": time.Now().Format(time.RFC3339),
		},
		Enabled: true,
	}

	if err := client.UpdateUser(ctx, username, updatedUser); err != nil {
		log.Printf("更新用户失败: %v", err)
	} else {
		fmt.Printf("用户 %s 更新成功\n", username)
	}
}

// 修改用户密码演示
func changeUserPasswordDemo(client *es.Client, ctx context.Context, username string) {
	newPassword := "NewDemoPass456!"
	if err := client.ChangeUserPassword(ctx, username, newPassword); err != nil {
		log.Printf("修改用户密码失败: %v", err)
	} else {
		fmt.Printf("用户 %s 密码修改成功\n", username)
	}
}

// 删除用户演示
func deleteUserDemo(client *es.Client, ctx context.Context, username string) {
	if err := client.DeleteUser(ctx, username); err != nil {
		log.Printf("删除用户失败: %v", err)
	} else {
		fmt.Printf("用户 %s 删除成功\n", username)
	}
}
