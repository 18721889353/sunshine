package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/elastic/go-elasticsearch/v7/esapi"
)

// Role 角色结构
type Role struct {
	Cluster           []string                 `json:"cluster,omitempty"`
	Indices           []RoleIndicesPermissions `json:"indices,omitempty"`
	Applications      []ApplicationPrivileges  `json:"applications,omitempty"`
	Global            interface{}              `json:"global,omitempty"`
	Metadata          map[string]interface{}   `json:"metadata,omitempty"`
	TransientMetadata map[string]interface{}   `json:"transient_metadata,omitempty"`
}

// RoleIndicesPermissions 索引权限
type RoleIndicesPermissions struct {
	Names                  []string       `json:"names"`
	Privileges             []string       `json:"privileges"`
	FieldSecurity          *FieldSecurity `json:"field_security,omitempty"`
	Query                  *string        `json:"query,omitempty"`
	AllowRestrictedIndices *bool          `json:"allow_restricted_indices,omitempty"`
}

// FieldSecurity 字段安全设置
type FieldSecurity struct {
	Grant  []string `json:"grant,omitempty"`
	Except []string `json:"except,omitempty"`
}

// ApplicationPrivileges 应用权限
type ApplicationPrivileges struct {
	Application string   `json:"application"`
	Privileges  []string `json:"privileges"`
	Resources   []string `json:"resources,omitempty"`
}

// User Elasticsearch 用户结构
type User struct {
	Username     string                 `json:"username,omitempty"`
	FullName     string                 `json:"full_name,omitempty"`
	Email        string                 `json:"email,omitempty"`
	Roles        []string               `json:"roles"`
	Password     string                 `json:"password,omitempty"`
	PasswordHash string                 `json:"password_hash,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Enabled      bool                   `json:"enabled"`
}

// GetRole 获取角色信息
func (c *Client) GetRole(ctx context.Context, roleName string) (*Role, error) {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "get_role", roleName)
	defer endSpan(nil)

	// 创建请求
	req := esapi.SecurityGetRoleRequest{
		Name: []string{roleName},
	}

	// 执行请求并处理响应
	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return nil, fmt.Errorf("get role error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("get role failed: %s", res.String())
		endSpan(err)
		return nil, err
	}

	// 读取响应体
	body, err := io.ReadAll(res.Body)
	if err != nil {
		endSpan(err)
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	// 解析响应
	var result map[string]Role
	if securityErr := json.Unmarshal(body, &result); securityErr != nil {
		endSpan(securityErr)
		return nil, fmt.Errorf("unmarshal role result error: %w", securityErr)
	}

	roleData, exists := result[roleName]
	if !exists {
		err = fmt.Errorf("role %s not found", roleName)
		endSpan(err)
		return nil, err
	}

	return &roleData, nil
}

// CreateRole 创建角色
func (c *Client) CreateRole(ctx context.Context, roleName string, role Role) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "create_role", roleName, role)
	defer endSpan(nil)

	// 使用缓冲池优化内存分配
	body := c.getBuffer()
	defer c.putBuffer(body)

	if err := json.NewEncoder(body).Encode(role); err != nil {
		endSpan(err)
		return fmt.Errorf("marshal role error: %w", err)
	}

	req := esapi.SecurityPutRoleRequest{
		Name: roleName,
		Body: bytes.NewReader(body.Bytes()),
	}

	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("create role error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("create role failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// UpdateRole 更新角色
func (c *Client) UpdateRole(ctx context.Context, roleName string, role Role) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "update_role", roleName, role)
	defer endSpan(nil)

	// 在 Elasticsearch 中，更新角色与创建角色使用相同的 API
	return c.CreateRole(ctx, roleName, role)
}

// DeleteRole 删除角色
func (c *Client) DeleteRole(ctx context.Context, roleName string) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "delete_role", roleName)
	defer endSpan(nil)

	req := esapi.SecurityDeleteRoleRequest{
		Name: roleName,
	}

	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("delete role error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("delete role failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// GetUser 获取用户信息
func (c *Client) GetUser(ctx context.Context, username string) (*User, error) {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "get_user", username)
	defer endSpan(nil)

	req := esapi.SecurityGetUserRequest{
		Username: []string{username},
	}

	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return nil, fmt.Errorf("get user error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("get user failed: %s", res.String())
		endSpan(err)
		return nil, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		endSpan(err)
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	// Elasticsearch安全API返回的用户信息格式是 { "username": { ...user_data... } }
	// 而不是 { "username": { "user": { ...user_data... } } }
	var result map[string]User

	if checkErr := json.Unmarshal(body, &result); checkErr != nil {
		endSpan(checkErr)
		return nil, fmt.Errorf("unmarshal user result error: %w", checkErr)
	}

	userData, exists := result[username]
	if !exists {
		err = fmt.Errorf("user %s not found", username)
		endSpan(err)
		return nil, err
	}

	userData.Username = username
	return &userData, nil
}

// CreateUser 创建用户
func (c *Client) CreateUser(ctx context.Context, username string, user User) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "create_user", username, user)
	defer endSpan(nil)

	// 使用缓冲池优化内存分配
	body := c.getBuffer()
	defer c.putBuffer(body)

	if err := json.NewEncoder(body).Encode(user); err != nil {
		endSpan(err)
		return fmt.Errorf("marshal user error: %w", err)
	}

	req := esapi.SecurityPutUserRequest{
		Username: username,
		Body:     bytes.NewReader(body.Bytes()),
	}

	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("create user error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("create user failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// UpdateUser 更新用户
func (c *Client) UpdateUser(ctx context.Context, username string, user User) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "update_user", username, user)
	defer endSpan(nil)

	// 在 Elasticsearch 中，更新用户与创建用户使用相同的 API
	return c.CreateUser(ctx, username, user)
}

// DeleteUser 删除用户
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "delete_user", username)
	defer endSpan(nil)

	req := esapi.SecurityDeleteUserRequest{
		Username: username,
	}

	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("delete user error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("delete user failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// ChangeUserPassword 修改用户密码
func (c *Client) ChangeUserPassword(ctx context.Context, username string, password string) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "change_user_password", username, password)
	defer endSpan(nil)

	// 创建请求体
	// 使用缓冲池优化内存分配
	body := c.getBuffer()
	defer c.putBuffer(body)

	if err := json.NewEncoder(body).Encode(map[string]string{
		"password": password,
	}); err != nil {
		endSpan(err)
		return fmt.Errorf("marshal password error: %w", err)
	}

	// 创建请求
	req := esapi.SecurityChangePasswordRequest{
		Username: username,
		Body:     bytes.NewReader(body.Bytes()),
	}

	// 执行请求并处理响应
	res, err := req.Do(ctx, c.Client)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("change password error: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("change password failed: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}
