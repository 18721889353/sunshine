package query

import "strings"

var defaultMaxSize = 10000

// SortIgnoreCount 排序忽略统计常量
// 当 Sort 设置为此值时，NewPage 会使用默认排序（id DESC），
// 但调用方可以通过 Page.sortIgnoreCount 字段判断是否跳过 COUNT 查询。
const SortIgnoreCount = "ignore count"

// SetMaxSize change the default maximum number of pages per page
func SetMaxSize(maxSize int) {
	if maxSize < 10 {
		maxSize = 10
	}
	defaultMaxSize = maxSize
}

// Page info
type Page struct {
	page            int    // page number, starting from page 0
	limit           int    // number per page
	sort            string // sort fields, default is id backwards
	sortIgnoreCount bool   // true 表示跳过 COUNT 查询（性能优化）
}

// Page get page value
func (p *Page) Page() int {
	return p.page
}

// Limit number per page
func (p *Page) Limit() int {
	return p.limit
}

// Size number per page
// Deprecated: use Limit instead
func (p *Page) Size() int {
	return p.limit
}

// Sort get sort field
func (p *Page) Sort() string {
	return p.sort
}

// Offset get offset value
func (p *Page) Offset() int {
	return p.page * p.limit
}

// SortIgnoreCount returns true if this page should skip COUNT query
func (p *Page) SortIgnoreCount() bool {
	return p.sortIgnoreCount
}

// DefaultPage default page, number 20 per page, sorted by id backwards
func DefaultPage(page int) *Page {
	if page < 0 {
		page = 0
	}
	return &Page{
		page:            page,
		limit:           20,
		sort:            "id DESC",
		sortIgnoreCount: false,
	}
}

// NewPage custom page, starting from page 0.
// the parameter columnNames indicates a sort field, if empty means id descending,
// if there are multiple column names, separated by a comma,
// a '-' sign in front of each column name indicates descending order, otherwise ascending order.
//
// Special value: if columnNames == "ignore count", the page will use default sort (id DESC)
// and set sortIgnoreCount = true, allowing the caller to skip COUNT query for better performance.
func NewPage(page int, limit int, columnNames string) *Page {
	if page < 0 {
		page = 0
	}
	if limit > defaultMaxSize || limit < 1 {
		limit = defaultMaxSize
	}

	// 特殊处理 SortIgnoreCount：使用默认排序，但标记跳过 Count
	if strings.TrimSpace(columnNames) == SortIgnoreCount {
		return &Page{
			page:            page,
			limit:           limit,
			sort:            "id DESC",
			sortIgnoreCount: true,
		}
	}

	return &Page{
		page:            page,
		limit:           limit,
		sort:            getSort(columnNames),
		sortIgnoreCount: false,
	}
}

// convert to mysql sort, each column name preceded by a '-' sign, indicating descending order, otherwise ascending order, example:
//
//	columnNames="name" means sort by name in ascending order,
//	columnNames="-name" means sort by name descending,
//	columnNames="name,age" means sort by name in ascending order, otherwise sort by age in ascending order,
//	columnNames="-name,-age" means sort by name descending before sorting by age descending.
func getSort(columnNames string) string {
	columnNames = strings.Replace(columnNames, " ", "", -1)
	if columnNames == "" {
		return "id DESC"
	}

	names := strings.Split(columnNames, ",")
	strs := make([]string, 0, len(names))
	for _, name := range names {
		if name[0] == '-' && len(name) > 1 {
			strs = append(strs, name[1:]+" DESC")
		} else {
			strs = append(strs, name+" ASC")
		}
	}

	return strings.Join(strs, ", ")
}
