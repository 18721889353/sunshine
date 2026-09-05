// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
	"github.com/pingcap/tidb/pkg/parser/mysql"
)

const (
	emptyCountSQL   = "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0"
	countSQLWrapper = "SELECT COUNT(*) FROM (%s) AS count_query"
)

var (
	globalRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	randMutex  sync.Mutex
)

// GetRandomExpireTime 生成随机化的缓存过期时间，防止缓存雪崩
func GetRandomExpireTime(base time.Duration) time.Duration {
	randMutex.Lock()
	offsetSeconds := globalRand.Int63n(RandomOffsetMaxSeconds) - RandomOffsetMinSeconds
	randMutex.Unlock()
	offset := time.Duration(offsetSeconds) * time.Second
	return base + offset
}

// parserPool 复用 TiDB Parser 对象
var parserPool = sync.Pool{
	New: func() any {
		return parser.New()
	},
}

// ConvertToCountSQL 将普通 SQL SELECT 查询转换为 COUNT 查询 SQL
// 现在返回 error，解析失败时不再静默降级
func ConvertToCountSQL(_ context.Context, sql string) (string, error) {
	rawSQL := strings.TrimSpace(sql)
	if rawSQL == "" {
		return emptyCountSQL, nil
	}

	pAny := parserPool.Get()
	p, ok := pAny.(*parser.Parser)
	if !ok {
		return "", errors.New("failed to assert parser type from pool")
	}
	defer parserPool.Put(p)

	p.SetSQLMode(mysql.ModeNone)
	stmt, err := p.ParseOneStmt(rawSQL, "", "")
	if err != nil {
		return "", fmt.Errorf("parse SQL failed: %w", err)
	}

	sel, ok := stmt.(*ast.SelectStmt)
	if !ok {
		return "", fmt.Errorf("statement is not SELECT")
	}

	// 剥离 ORDER BY, LIMIT 和 FOR UPDATE / LOCK IN SHARE MODE（计数不需要）
	sel.OrderBy = nil
	sel.Limit = nil
	sel.LockInfo = nil

	hasGroupBy := sel.GroupBy != nil
	hasHaving := sel.Having != nil
	hasDistinct := sel.Distinct

	if !hasGroupBy && !hasHaving && !hasDistinct {
		countOne := &ast.AggregateFuncExpr{
			F: ast.AggFuncCount,
			Args: []ast.ExprNode{
				ast.NewValueExpr(1, "", ""),
			},
		}
		sel.Fields = &ast.FieldList{
			Fields: []*ast.SelectField{
				{Expr: countOne},
			},
		}
	}

	sb := &strings.Builder{}
	flags := format.DefaultRestoreFlags |
		format.RestoreKeyWordUppercase |
		format.RestoreStringSingleQuotes |
		format.RestoreNameBackQuotes
	restoreCtx := format.NewRestoreCtx(flags, sb)
	if err := sel.Restore(restoreCtx); err != nil {
		return "", fmt.Errorf("restore AST failed: %w", err)
	}

	cleanedSQL := sb.String()

	if !hasGroupBy && !hasHaving && !hasDistinct {
		return cleanedSQL, nil
	}
	return fmt.Sprintf(countSQLWrapper, cleanedSQL), nil
}

// BuildLockKey 构建分布式锁的 key（前缀不应包含末尾冒号）
func BuildLockKey(prefix, key string) string {
	if key == "" {
		return prefix
	}
	if prefix == "" {
		return key
	}
	var builder strings.Builder
	builder.Grow(len(prefix) + 1 + len(key))
	builder.WriteString(prefix)
	builder.WriteString(":")
	builder.WriteString(key)
	return builder.String()
}

// GetObjectID 通过反射获取对象的 ID 字段（uint64）
func GetObjectID(obj interface{}) uint64 {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0
	}
	field := v.FieldByName("ID")
	if !field.IsValid() {
		return 0
	}
	if field.Kind() == reflect.Uint64 {
		return field.Uint()
	}
	return 0
}
