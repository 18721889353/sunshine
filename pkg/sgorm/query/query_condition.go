// Package query 是一个自定义条件查询库，支持复杂的分页查询。
package query

import (
	"fmt"
	"strings"
)

const (
	// Eq 等于
	Eq = "eq"
	// Neq 不等于
	Neq = "neq"
	// Gt 大于
	Gt = "gt"
	// Gte 大于等于
	Gte = "gte"
	// Lt 小于
	Lt = "lt"
	// Lte 小于等于
	Lte = "lte"
	// Like 模糊查询
	Like = "like"
	// In 包含
	In = "in"
	// NotIN 不包含
	NotIN = "notin"
	// IsNull 是否为空
	IsNull = "isnull"
	// IsNotNull 是否不为空
	IsNotNull = "isnotnull"

	// AND 逻辑与
	AND string = "and"
	// OR 逻辑或
	OR string = "or"
)

var expMap = map[string]string{
	Eq:        " = ",           // 等于
	Neq:       " <> ",          // 不等于
	Gt:        " > ",           // 大于
	Gte:       " >= ",          // 大于等于
	Lt:        " < ",           // 小于
	Lte:       " <= ",          // 小于等于
	Like:      " LIKE ",        // 模糊查询
	In:        " IN ",          // 包含
	NotIN:     " NOT IN ",      // 不包含
	IsNull:    " IS NULL ",     // 是否为空
	IsNotNull: " IS NOT NULL ", // 是否不为空

	"=":           " = ",           // 等于
	"!=":          " <> ",          // 不等于
	">":           " > ",           // 大于
	">=":          " >= ",          // 大于等于
	"<":           " < ",           // 小于
	"<=":          " <= ",          // 小于等于
	"not in":      " NOT IN ",      // 不包含
	"is null":     " IS NULL ",     // 是否为空
	"is not null": " IS NOT NULL ", // 是否不为空
}

var logicMap = map[string]string{
	AND: " AND ", // 逻辑与
	OR:  " OR ",  // 逻辑或

	"&":   " AND ", // 逻辑与
	"&&":  " AND ", // 逻辑与
	"|":   " OR ",  // 逻辑或
	"||":  " OR ",  // 逻辑或
	"AND": " AND ", // 逻辑与
	"OR":  " OR ",  // 逻辑或
}

// Params 查询参数结构体
type Params struct {
	Page  int    `json:"page" form:"page" binding:"gte=0"`      // 分页页码，默认从0开始
	Limit int    `json:"limit" form:"limit" binding:"gte=1"`    // 每页条数，最小值为1
	Sort  string `json:"sort,omitempty" form:"sort" binding:""` // 排序字段

	Columns []Column `json:"columns,omitempty" form:"columns"` // 查询列信息，非必填

	// Deprecated: 在sunshine版本v1.8.6中建议使用Limit代替Size，未来将移除
	Size int `json:"size" form:"size"`
}

// Column 查询列信息结构体
type Column struct {
	Name  string      `json:"name" form:"name"`   // 列名
	Exp   string      `json:"exp" form:"exp"`     // 表达式，默认值为"=", 支持 =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
	Value interface{} `json:"value" form:"value"` // 列值
	Logic string      `json:"logic" form:"logic"` // 逻辑运算符，默认为"and"，支持 &(and), ||(or)
}

// checkValid 校验列信息是否合法
func (c *Column) checkValid() error {
	if c.Name == "" {
		return fmt.Errorf("字段 'name' 不能为空")
	}
	if c.Value == nil {
		v := expMap[strings.ToLower(c.Exp)]
		if v == " IS NULL " || v == " IS NOT NULL " {
			return nil
		}
		return fmt.Errorf("字段 'value' 不能为空")
	}
	return nil
}

// convert 将表达式类型转换为SQL表达式，并将逻辑运算符转换为SQL使用的字符
func (c *Column) convert() (string, error) {
	symbol := "?"
	if c.Exp == "" {
		c.Exp = Eq
	}
	if v, ok := expMap[strings.ToLower(c.Exp)]; ok { //nolint
		c.Exp = v
		switch c.Exp {
		case " LIKE ":
			val, ok1 := c.Value.(string)
			if !ok1 {
				return symbol, fmt.Errorf("无效的值类型 '%s'", c.Value)
			}
			l := len(val)
			if l > 2 {
				val2 := val[1 : l-1]
				val2 = strings.ReplaceAll(val2, "%", "\\%")
				val2 = strings.ReplaceAll(val2, "_", "\\_")
				val = string(val[0]) + val2 + string(val[l-1])
			}
			if strings.HasPrefix(val, "%") ||
				strings.HasPrefix(val, "_") ||
				strings.HasSuffix(val, "%") ||
				strings.HasSuffix(val, "_") {
				c.Value = val
			} else {
				c.Value = "%" + val + "%"
			}
		case " IN ", " NOT IN ":
			val, ok1 := c.Value.(string)
			if !ok1 {
				return symbol, fmt.Errorf("无效的值类型 '%s'", c.Value)
			}
			iVal := []interface{}{}
			ss := strings.Split(val, ",")
			for _, s := range ss {
				iVal = append(iVal, s)
			}
			c.Value = iVal
			symbol = "(?)"
		case " IS NULL ", " IS NOT NULL ":
			c.Value = nil
			symbol = ""
		}
	} else {
		return symbol, fmt.Errorf("不支持的表达式类型 '%s'", c.Exp)
	}

	if c.Logic == "" {
		c.Logic = AND
	}
	if v, ok := logicMap[strings.ToLower(c.Logic)]; ok { //nolint
		c.Logic = v
	} else {
		return symbol, fmt.Errorf("未知的逻辑类型 '%s'", c.Logic)
	}

	return symbol, nil
}

// ConvertToPage 转换为分页参数
func (p *Params) ConvertToPage() (order string, limit int, offset int) { //nolint
	page := NewPage(p.Page, p.Limit, p.Sort)
	order = page.sort
	limit = page.limit
	offset = page.page * page.limit
	return //nolint
}

// ConvertToGormConditions 将查询条件转换为 GORM 兼容的参数
// 忽略最后一列的逻辑类型，无论是单列还是多列查询
func (p *Params) ConvertToGormConditions() (string, []interface{}, error) {
	str := ""
	args := []interface{}{}
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

		if i == l-1 { // 忽略最后一列的逻辑类型
			str += column.Name + column.Exp + symbol
		} else {
			str += column.Name + column.Exp + symbol + column.Logic
		}
		if column.Value != nil {
			args = append(args, column.Value)
		}
		// 当多个列相同时，判断是否使用 IN
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
		str = field + " IN (?)"
		args = []interface{}{args}
	}

	return str, args, nil
}

// Conditions 查询条件结构体
type Conditions struct {
	Columns []Column `json:"columns" form:"columns" binding:"min=1"` // 列信息
}

// CheckValid 校验查询条件是否合法
func (c *Conditions) CheckValid() error {
	if len(c.Columns) == 0 {
		return fmt.Errorf("字段 'columns' 不能为空")
	}

	for _, column := range c.Columns {
		err := column.checkValid()
		if err != nil {
			return err
		}
		if column.Exp != "" {
			if _, ok := expMap[column.Exp]; !ok {
				return fmt.Errorf("未知的表达式类型 '%s'", column.Exp)
			}
		}
		if column.Logic != "" {
			if _, ok := logicMap[column.Logic]; !ok {
				return fmt.Errorf("未知的逻辑类型 '%s'", column.Logic)
			}
		}
	}

	return nil
}

// ConvertToGorm 将查询条件转换为 GORM 兼容的参数
// 忽略最后一列的逻辑类型，无论是单列还是多列查询
func (c *Conditions) ConvertToGorm() (string, []interface{}, error) {
	p := &Params{Columns: c.Columns}
	return p.ConvertToGormConditions()
}
