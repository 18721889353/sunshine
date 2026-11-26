// Package query is a library of custom condition queries, support for complex conditional paging queries.
package query

import (
	"fmt"
	"strings"
	"time"
)

const (
	// Eq equal
	Eq = "eq"
	// Neq not equal
	Neq = "neq"
	// Gt greater than
	Gt = "gt"
	// Gte greater than or equal
	Gte = "gte"
	// Lt less than
	Lt = "lt"
	// Lte less than or equal
	Lte = "lte"
	// Like fuzzy lookup
	Like = "like"
	// In include
	In = "in"
	// NotIN not include
	NotIN = "notin"
	// IsNull is null
	IsNull = "isnull"
	// IsNotNull is not null
	IsNotNull = "isnotnull"

	// AND logic and
	AND string = "and"
	// OR logic or
	OR string = "or"
)

// SQL 片段常量：expMap/logicMap 的取值与 checkValid/convert 里的判定共用同一份定义，
// 避免同一个片段在映射表和 switch/if 里各写一遍而漂移。
const (
	sqlExpLike      = " LIKE "
	sqlExpIn        = " IN "
	sqlExpNotIn     = " NOT IN "
	sqlExpIsNull    = " IS NULL "
	sqlExpIsNotNull = " IS NOT NULL "

	sqlLogicAnd = " AND "
	sqlLogicOr  = " OR "
)

var expMap = map[string]string{
	Eq:        " = ",
	Neq:       " <> ",
	Gt:        " > ",
	Gte:       " >= ",
	Lt:        " < ",
	Lte:       " <= ",
	Like:      sqlExpLike,
	In:        sqlExpIn,
	NotIN:     sqlExpNotIn,
	IsNull:    sqlExpIsNull,
	IsNotNull: sqlExpIsNotNull,

	"=":           " = ",
	"!=":          " <> ",
	">":           " > ",
	">=":          " >= ",
	"<":           " < ",
	"<=":          " <= ",
	"not in":      sqlExpNotIn,
	"is null":     sqlExpIsNull,
	"is not null": sqlExpIsNotNull,
}

var logicMap = map[string]string{
	AND: sqlLogicAnd,
	OR:  sqlLogicOr,

	"&":   sqlLogicAnd,
	"&&":  sqlLogicAnd,
	"|":   sqlLogicOr,
	"||":  sqlLogicOr,
	"AND": sqlLogicAnd,
	"OR":  sqlLogicOr,

	"and:(": sqlLogicAnd,
	"and:)": sqlLogicAnd,
	"or:(":  sqlLogicOr,
	"or:)":  sqlLogicOr,
}

// Params query parameters
type Params struct {
	Page  int    `json:"page" form:"page" binding:"gte=0"`
	Limit int    `json:"limit" form:"limit" binding:"gte=1"`
	Sort  string `json:"sort,omitempty" form:"sort" binding:""`

	Columns []Column `json:"columns,omitempty" form:"columns"` // not required
}

// Column query info
type Column struct {
	Name  string      `json:"name" form:"name"`   // column name
	Exp   string      `json:"exp" form:"exp"`     // expressions, default value is "=", support =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
	Value interface{} `json:"value" form:"value"` // column value
	Logic string      `json:"logic" form:"logic"` // logical type, defaults to and when the value is null, with &(and), ||(or)
}

func (c *Column) checkValid() error {
	if c.Name == "" {
		return fmt.Errorf("field 'name' cannot be empty")
	}
	if c.Value == nil {
		v := expMap[strings.ToLower(c.Exp)]
		if v == sqlExpIsNull || v == sqlExpIsNotNull {
			return nil
		}
		return fmt.Errorf("field 'value' cannot be nil")
	}
	return nil
}

// converting ExpType to sql expressions and LogicType to sql using characters
func (c *Column) convert() (string, error) {
	symbol := "?"
	if c.Exp == "" {
		c.Exp = Eq
	}
	if v, ok := expMap[strings.ToLower(c.Exp)]; ok { //nolint
		c.Exp = v
		switch c.Exp {
		case sqlExpLike:
			val, ok1 := c.Value.(string)
			if !ok1 {
				return symbol, fmt.Errorf("invalid value type '%s'", c.Value)
			}
			// 转换为rune数组处理unicode字符
			runes := []rune(val)
			if len(runes) == 0 {
				return symbol, fmt.Errorf("empty string value")
			}
			l := len(runes)
			if l > 2 {
				middle := string(runes[1 : len(runes)-1])
				middle = strings.ReplaceAll(middle, "%", "\\%")
				middle = strings.ReplaceAll(middle, "_", "\\_")
				val = string(runes[0]) + middle + string(runes[len(runes)-1])
			}
			// 判断首尾字符时需要转换为string处理
			firstChar := string(runes[0])
			lastChar := string(runes[len(runes)-1])
			if !(firstChar == "%" || firstChar == "_") &&
				!(lastChar == "%" || lastChar == "_") {
				val = "%" + val + "%"
			}
			c.Value = val
		case sqlExpIn, sqlExpNotIn:
			val, ok1 := c.Value.(string)
			if !ok1 {
				return symbol, fmt.Errorf("invalid value type '%s'", c.Value)
			}
			iVal := []interface{}{}
			ss := strings.Split(val, ",")
			for _, s := range ss {
				iVal = append(iVal, s)
			}
			c.Value = iVal
			symbol = "(?)"
		case sqlExpIsNull, sqlExpIsNotNull:
			c.Value = nil
			symbol = ""
		}
	} else {
		return symbol, fmt.Errorf("unsupported exp type '%s'", c.Exp)
	}

	if c.Logic == "" {
		c.Logic = AND
	} else {
		logic := strings.ToLower(c.Logic)
		if _, ok := logicMap[logic]; ok { //nolint
			c.Logic = logic
		} else {
			return symbol, fmt.Errorf("unsupported logic type '%s'", c.Logic)
		}
	}

	return symbol, nil
}

// ConvertToPage converted to page
func (p *Params) ConvertToPage() (order string, limit int, offset int) { //nolint
	page := NewPage(p.Page, p.Limit, p.Sort)
	order = page.sort
	limit = page.limit
	offset = page.page * page.limit
	return //nolint
}

// ConvertToGormConditions conversion to gorm-compliant parameters based on the Columns parameter
// ignore the logical type of the last column, whether it is a one-column or multi-column query
func (p *Params) ConvertToGormConditions() (cond string, args []interface{}, err error) {
	// 指标埋点：条件构建耗时（result=ok/error）。
	// 用命名返回值 + defer，覆盖本函数全部 return 路径（含 checkValid/convert 失败）。
	start := time.Now()
	defer func() { observeQueryBuild(err, time.Since(start)) }()

	// strings.Builder：SQL 条件拼接是查询热路径，
	// 用 str += 会在每次循环整体复制（O(n²)），Builder 累积为 O(n)
	var strB strings.Builder
	strB.Grow(len(p.Columns) * 24) // 每列约 24 字节（列名+表达式+符号+逻辑符），预估避免多次扩容
	args = []interface{}{}
	l := len(p.Columns)
	if l == 0 {
		return "", nil, nil
	}

	isUseIN := true
	if l == 1 {
		isUseIN = false
	}
	field := p.Columns[0].Name

	for i, column := range p.Columns {
		if err := column.checkValid(); err != nil {
			return "", nil, err
		}

		symbol, err := column.convert()
		if err != nil {
			return "", nil, err
		}

		if i == l-1 { // ignore the logical type of the last column
			switch column.Logic {
			case "or:)", "and:)":
				strB.WriteString(column.Name + column.Exp + symbol + " ) ")
			default:
				strB.WriteString(column.Name + column.Exp + symbol)
			}
		} else {
			switch column.Logic {
			case "or:(", "and:(":
				strB.WriteString(" ( " + column.Name + column.Exp + symbol + logicMap[column.Logic])
			case "or:)", "and:)":
				strB.WriteString(column.Name + column.Exp + symbol + " ) " + logicMap[column.Logic])
			default:
				strB.WriteString(column.Name + column.Exp + symbol + logicMap[column.Logic])
			}
		}
		if column.Value != nil {
			args = append(args, column.Value)
		}
		// when multiple columns are the same, determine whether the use of IN
		if isUseIN {
			if field != column.Name {
				isUseIN = false
				continue
			}
			if column.Exp != expMap[Eq] {
				isUseIN = false
			}
		}
	}

	if isUseIN {
		// 覆盖路径：直接构造返回，不经过 Builder
		return field + " IN (?)", []interface{}{args}, nil
	}

	return strB.String(), args, nil
}

// Conditions query conditions
type Conditions struct {
	Columns []Column `json:"columns" form:"columns" binding:"min=1"` // columns info
}

// ConvertToGorm conversion to gorm-compliant parameters based on the Columns parameter
// ignore the logical type of the last column, whether it is a one-column or multi-column query
func (c *Conditions) ConvertToGorm() (string, []interface{}, error) {
	p := &Params{Columns: c.Columns}
	return p.ConvertToGormConditions()
}

// CheckValid check valid
// CheckValid 校验查询条件是否合法（只读，不修改入参）。
// 校验列名/取值/表达式/逻辑符：Exp/Logic 判定与 convert 共用 expMap/logicMap，
// 在构造 SQL 之前拦截非法入参（等 convert 才报错时部分 SQL 已拼接）。
func (c *Conditions) CheckValid() error {
	if len(c.Columns) == 0 {
		return fmt.Errorf("field 'columns' cannot be empty")
	}
	for i := range c.Columns {
		col := &c.Columns[i]
		if err := col.checkValid(); err != nil {
			return err
		}
		if col.Exp != "" {
			if _, ok := expMap[strings.ToLower(col.Exp)]; !ok {
				return fmt.Errorf("unsupported exp type '%s'", col.Exp)
			}
		}
		if col.Logic != "" {
			if _, ok := logicMap[strings.ToLower(col.Logic)]; !ok {
				return fmt.Errorf("unsupported logic type '%s'", col.Logic)
			}
		}
	}
	return nil
}
