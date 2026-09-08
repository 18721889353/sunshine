// Package dao 提供泛型数据访问层基础组件
// 这个包包含了整个 ORM 缓存框架最底层的工具函数
package dao

import (
	"context"   // 上下文，用于传递请求超时、取消信号等（这里主要为了函数签名兼容，实际未使用）
	"errors"    // 标准错误库，用于创建和判断错误
	"fmt"       // 格式化字符串，用于拼接错误信息
	"math/rand" // 随机数生成器，用来生成随机偏移量，防止缓存雪崩
	"reflect"   // 反射包，允许程序在运行时动态地查看和操作变量的类型和字段（这里用来抓取结构体的 ID）
	"strings"   // 字符串操作库，用来 Trim（去空格）、Builder（高效拼接）
	"sync"      // 同步原语，提供了互斥锁（Mutex）和对象池（Pool）
	"time"      // 时间库，用来获取纳秒时间戳作为随机种子，以及处理时间间隔

	"github.com/pingcap/tidb/pkg/parser"        // TiDB 的 SQL 解析器，能把 SQL 字符串变成 AST 语法树
	"github.com/pingcap/tidb/pkg/parser/ast"    // AST（抽象语法树）的节点定义，比如 SelectStmt、AggregateFuncExpr
	"github.com/pingcap/tidb/pkg/parser/format" // 把 AST 还原成 SQL 字符串时的格式化工具（大写关键字、加引号等）
	"github.com/pingcap/tidb/pkg/parser/mysql"  // MySQL 相关的常量，比如 SQL 模式（ModeNone）
)

// ============================================================================
// 第一部分：SQL 模板常量
// ============================================================================

const (
	// emptyCountSQL：这是一个必定返回 0 的"假 SQL"。
	// 用途：当传进来的 SQL 是空字符串时，用这个占位，确保程序不报错，且语法绝对合法。
	// 逻辑：先造一个只有一行数字 1 的临时表，然后 WHERE 1=0（永远为假），COUNT 数空表，结果必然为 0。
	emptyCountSQL = "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0"

	// countSQLWrapper：这是一个 SQL 套娃模板。
	// 用途：当原 SQL 包含 GROUP BY、HAVING、DISTINCT 等"变形操作"时，不能简单改成 COUNT(1)。
	// 必须把原 SQL 整个当成一张临时表（子查询），在外面数行数。
	// 例如：原 SQL 是 "SELECT ... GROUP BY ..."，套娃后就变成 "SELECT COUNT(*) FROM (原SQL) AS count_query"
	countSQLWrapper = "SELECT COUNT(*) FROM (%s) AS count_query"
)

// ============================================================================
// 第二部分：全局随机数生成器（防缓存雪崩）
// ============================================================================

var (
	// globalRand：全局随机数生成器。
	// 这里用 time.Now().UnixNano()（当前时间的纳秒数）作为种子，确保每次程序启动，摇出的随机数序列都不同。
	// 注意：math/rand 包默认不是并发安全的。多个协程同时调用 globalRand.Int63n() 会数据竞争。
	globalRand = rand.New(rand.NewSource(time.Now().UnixNano()))

	// globalRandMutex：一把互斥锁，专门用来保护 globalRand。
	// 任何协程要使用 globalRand 之前，都必须先调用 globalRandMutex.Lock()，用完再 Unlock()。
	// 这保证了同一时刻只有一个协程在摇号，其他协程乖乖排队。
	globalRandMutex sync.Mutex
)

// GetRandomExpireTime 生成随机化的缓存过期时间，防止缓存雪崩。
// 参数 base 是基础过期时间（比如 30 分钟）。
// 返回：base 加上一个 -300 到 +300 秒之间的随机偏移量。
//
// 为什么需要随机偏移？
// 如果 1000 个 key 的过期时间都是 30 分钟整，它们会在同一时刻集体失效，
// 数据库瞬间被海量请求打崩。加一个随机偏移（±5分钟），让过期时间分散开。
func GetRandomExpireTime(base time.Duration) time.Duration {
	// 第一步：加锁，防止多个协程同时操作 globalRand 导致数据错乱
	globalRandMutex.Lock()
	// 第二步：摇号。Int63n(600) 生成 0~599 之间的随机整数，减去 300 后得到 -300 ~ +299。
	// RandomOffsetMaxSeconds=600, RandomOffsetMinSeconds=300（定义在 cache_constants.go 里）
	offsetSeconds := globalRand.Int63n(RandomOffsetMaxSeconds) - RandomOffsetMinSeconds
	// 第三步：解锁，让下一个排队的协程进来摇号
	globalRandMutex.Unlock()
	// 第四步：把秒数转换成 time.Duration 类型，加到基础时间上，返回结果
	offset := time.Duration(offsetSeconds) * time.Second
	return base + offset
}

// ============================================================================
// 第三部分：SQL 解析器对象池（复用解析器，提升性能）
// ============================================================================

// parserPool：这是一个 sync.Pool 对象池，用来复用 TiDB 的 SQL 解析器。
// 因为 parser.New() 创建解析器比较耗费 CPU，频繁创建销毁会导致垃圾回收（GC）压力巨大。
// 这里的 New 函数定义了解析器"无中生有"的方法：调用 parser.New() 造一个新的。
//
// 使用方式：从池子里 Get() 一个解析器，用完 Put() 放回去。
// 这样解析器被反复使用，大幅降低内存分配和 GC 压力。
var parserPool = sync.Pool{
	New: func() any {
		return parser.New()
	},
}

// ============================================================================
// 第四部分：核心函数 - SQL 转 COUNT SQL
// ============================================================================

// ConvertToCountSQL 将普通的 SELECT 查询语句转换为等价的 COUNT 计数语句。
// 参数：ctx（上下文，这里没用到，占位）、sql（要转换的原始 SQL 字符串）。
// 返回：转换后的计数 SQL，或者错误。
// 这个函数是分页查询（GetByColumns）中计算总条数（total）的关键。
//
// 工作流程：
//  1. 用 TiDB Parser 把 SQL 字符串解析成 AST（抽象语法树）
//  2. 砍掉计数不需要的节点（ORDER BY、LIMIT、锁）
//  3. 判断是简单查询（无 GROUP BY/DISTINCT）还是复杂查询
//  4. 简单查询：把 SELECT 字段替换成 COUNT(1)
//  5. 复杂查询：保留原 SQL，外面套一层 SELECT COUNT(*) FROM (...) AS count_query
//  6. 把修改后的 AST 还原成 SQL 字符串返回
//
// 示例：
//
//	输入："SELECT id, name FROM users WHERE age > 18 ORDER BY id LIMIT 10"
//	输出："SELECT COUNT(1) FROM users WHERE age > 18"
//
//	输入："SELECT department FROM users GROUP BY department"
//	输出："SELECT COUNT(*) FROM (SELECT department FROM users GROUP BY department) AS count_query"
func ConvertToCountSQL(_ context.Context, sql string) (string, error) {
	// 1. 去掉首尾空格，得到纯净的 SQL
	rawSQL := strings.TrimSpace(sql)

	// 2. 如果传的是空字符串，直接返回那个必定为 0 的占位 SQL，避免解析报错
	if rawSQL == "" {
		return emptyCountSQL, nil
	}

	// 3. 从对象池里借一个解析器出来
	// Get() 返回的是 interface{} 空接口，需要类型断言成 *parser.Parser
	pAny := parserPool.Get()
	p, ok := pAny.(*parser.Parser)
	// 如果断言失败（理论上不可能），返回错误
	if !ok {
		return "", errors.New("failed to assert parser type from pool")
	}
	// 4. 关键：用 defer 确保函数返回前，把解析器放回池子里（借了要还）
	// 不管中间是报错还是正常返回，defer 都会执行 Put
	defer parserPool.Put(p)

	// 5. 设置解析器的 SQL 模式为 ModeNone（最宽松模式）
	// 这意味着解析器容忍各种 MySQL 方言和特殊写法，不会因为语法严苛而报错
	p.SetSQLMode(mysql.ModeNone)

	// 6. 正式解析 SQL 字符串
	// 参数：rawSQL（要解析的字符串），空字符串（默认字符集），空字符串（默认排序规则）
	// 返回：AST 语法树的根节点 stmt，和可能的错误 err
	stmt, err := p.ParseOneStmt(rawSQL, "", "")
	if err != nil {
		// 如果解析失败（比如 SQL 语法严重错误），包装错误信息返回
		return "", fmt.Errorf("parse SQL failed: %w", err)
	}

	// 7. 类型断言：我们确信传入的是 SELECT 语句，所以把通用节点 stmt 转成具体的 *ast.SelectStmt
	// 如果是 INSERT、UPDATE 等其他类型，转换失败（ok == false），直接报错退出
	sel, ok := stmt.(*ast.SelectStmt)
	if !ok {
		return "", fmt.Errorf("statement is not SELECT")
	}

	// 8. 瘦身操作：删除计数不需要的节点
	// 8.1 OrderBy（排序）：计数不需要排序，砍掉
	sel.OrderBy = nil
	// 8.2 Limit（分页）：计数必须算总数，不能只数前 10 条，砍掉
	sel.Limit = nil
	// 8.3 LockInfo（行锁，如 FOR UPDATE）：计数只是读一下数量，不需要加锁，砍掉
	sel.LockInfo = nil

	// 9. 判断这个 SQL 是不是"复杂查询"
	// 9.1 GroupBy 是否不为空（有没有分组）
	hasGroupBy := sel.GroupBy != nil
	// 9.2 Having 是否不为空（有没有分组后的筛选）
	hasHaving := sel.Having != nil
	// 9.3 Distinct 是否为 true（有没有去重）
	hasDistinct := sel.Distinct

	// 10. 核心逻辑：如果是"简单查询"（没有分组、没有去重、没有 Having）
	if !hasGroupBy && !hasHaving && !hasDistinct {
		// 10.1 造一块"COUNT(1)"的 AST 积木
		countOne := &ast.AggregateFuncExpr{
			// F 字段指定这是 COUNT 聚合函数
			F: ast.AggFuncCount,
			// Args 是参数列表，NewValueExpr(1, "", "") 代表数字 1
			// 后面两个空字符串是因为数字不需要字符集和排序规则
			Args: []ast.ExprNode{
				ast.NewValueExpr(1, "", ""),
			},
		}
		// 10.2 把原来 SELECT 后面的字段列表（id, name...）整坨换成这个 COUNT(1)
		// 也就是：原来展架上放着多块积木，现在扔掉，只放一块 COUNT(1)
		sel.Fields = &ast.FieldList{
			Fields: []*ast.SelectField{
				{Expr: countOne},
			},
		}
	}

	// 11. 准备一个高性能的"写字板"（字符串缓冲区），用来接收还原后的 SQL
	sb := &strings.Builder{}

	// 12. 设置还原格式的"规则清单"（flags）
	// 规则 1：format.DefaultRestoreFlags -> 继承 TiDB 默认的还原规则
	// 规则 2：format.RestoreKeyWordUppercase -> 关键字大写（SELECT、FROM 变成大写）
	// 规则 3：format.RestoreStringSingleQuotes -> 字符串用单引号包起来（name = '张三'）
	// 规则 4：format.RestoreNameBackQuotes -> 表名/字段名用反引号包起来（`users`、`id`）
	flags := format.DefaultRestoreFlags |
		format.RestoreKeyWordUppercase |
		format.RestoreStringSingleQuotes |
		format.RestoreNameBackQuotes

	// 13. 用上面配置好的"规则清单"和"写字板"，创建一个还原上下文（restoreCtx）
	restoreCtx := format.NewRestoreCtx(flags, sb)

	// 14. 调用 sel.Restore()，让解析器遍历这棵 AST 语法树，按照 rules 把 SQL 写到 sb 里
	if err := sel.Restore(restoreCtx); err != nil {
		return "", fmt.Errorf("restore AST failed: %w", err)
	}

	// 15. 把写字板里的内容取出来，变成字符串
	cleanedSQL := sb.String()

	// 16. 最后一步：判断是否需要套娃
	if !hasGroupBy && !hasHaving && !hasDistinct {
		// 简单查询：刚才已经把 SELECT 字段改成了 COUNT(1)，直接返回 cleanedSQL
		// 例如：返回 "SELECT COUNT(1) FROM users WHERE age > 18"
		return cleanedSQL, nil
	}
	// 复杂查询：刚才没改 SELECT 字段，保留原始字段（如 department）
	// 必须套一层子查询：SELECT COUNT(*) FROM (cleanedSQL) AS count_query
	// 例如：返回 "SELECT COUNT(*) FROM (SELECT department FROM users GROUP BY department) AS count_query"
	return fmt.Sprintf(countSQLWrapper, cleanedSQL), nil
}

// ============================================================================
// 第五部分：工具函数 - 拼接分布式锁 Key
// ============================================================================

// BuildLockKey 构建分布式锁的 Redis Key。
// 参数 prefix 是前缀（如 "lock:refresh"），key 是具体的标识（如 "123" 或 "condition:xxx"）。
// 返回拼接好的字符串，中间用冒号分隔，例如 "lock:refresh:123"。
// 注意：如果传入的 key 为空，直接返回 prefix；如果 prefix 为空，直接返回 key。
//
// 为什么不用 + 拼接？
// 在高并发场景下，用 strings.Builder 提前分配好内存（Grow），
// 比用 + 反复创建新字符串要高效得多。
func BuildLockKey(prefix, key string) string {
	// 如果 key 是空字符串，没必要加冒号，直接返回 prefix
	if key == "" {
		return prefix
	}
	// 如果 prefix 是空字符串，直接返回 key
	if prefix == "" {
		return key
	}
	// 高效拼接：使用 strings.Builder，并提前用 Grow 分配好内存大小（prefix长度 + 1个冒号 + key长度）
	var builder strings.Builder
	builder.Grow(len(prefix) + 1 + len(key))
	builder.WriteString(prefix)
	builder.WriteString(":")
	builder.WriteString(key)
	return builder.String()
}

// ============================================================================
// 第六部分：工具函数 - 反射获取 ID
// ============================================================================

// GetObjectID 通过反射从任意结构体中取出 ID 字段（必须为 uint64 类型）。
// 参数 obj：任意类型的对象（可以是结构体，也可以是指向结构体的指针）。
// 返回：找到的 uint64 ID，如果找不到或类型不匹配返回 0。
//
// 为什么要用反射？
// 因为 BaseDao 是泛型 T，Go 在编译时不知道 T 有哪些字段，只能运行时靠反射去抓。
//
// 注意：这个函数强制要求 ID 字段必须是 uint64 类型。
// 这样设计是为了全栈统一 ID 类型，防止 uint32 溢出或类型转换混乱。
func GetObjectID(obj interface{}) uint64 {
	// 1. 获取对象的反射值（Value）
	v := reflect.ValueOf(obj)

	// 2. 如果传进来的是指针（比如 &User{ID:1}），调用 Elem() 钻进去，拿到指针指向的实际结构体
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// 3. 如果钻进去后还不是结构体（比如传了个数字或字符串），返回 0
	if v.Kind() != reflect.Struct {
		return 0
	}

	// 4. 按字段名 "ID" 去查找结构体里的字段
	field := v.FieldByName("ID")

	// 5. 如果没找到叫 "ID" 的字段，返回 0
	if !field.IsValid() {
		return 0
	}

	// 6. 检查字段的类型是不是 uint64
	if field.Kind() == reflect.Uint64 {
		// 7. 是的话，调用 Uint() 方法取出具体的值返回
		return field.Uint()
	}

	// 8. 类型不是 uint64，返回 0（保证系统安全，不乱转类型）
	return 0
}

// ============================================================================
// 第七部分：工具函数 - 命名转换
// ============================================================================

// UnderscoreToCamel 把蛇形命名（user_example）转成驼峰命名（UserExample）
// 这是为了生成 Redis Key 前缀时，用驼峰更美观。
//
// 转换规则：
//   - 按下划线分割字符串：["user", "example"]
//   - 从第二个单词开始，首字母大写：["user", "Example"]
//   - 拼接后返回："UserExample"
//
// 使用场景：
//   - 表名 "users" -> "Users"
//   - 表名 "sys_user_example" -> "SysUserExample"
//   - 生成的 Redis Key 前缀：data:SysUserExample:123
//
// 这个函数原本是 cache_manager.go 里的私有函数，
// 现在提升到 utils.go 并改为公开函数，方便包外其他模块复用。
func UnderscoreToCamel(s string) string {
	// 按下划线分割：["user", "example"]
	parts := strings.Split(s, "_")
	// 从第二个单词开始，首字母大写
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	// 拼接后返回："UserExample"
	return strings.Join(parts, "")
}
