package scopes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScopeFunctions(t *testing.T) {
	// 测试 ForceMaster
	s := ForceMaster()
	assert.NotNil(t, s)

	// 测试 Unscoped
	s = Unscoped()
	assert.NotNil(t, s)

	// 测试 Preload
	s = Preload("Profile")
	assert.NotNil(t, s)

	// 测试 Select
	s = Select("id", "name")
	assert.NotNil(t, s)

	// 测试 Order
	s = Order("id DESC")
	assert.NotNil(t, s)

	// 测试 Limit
	s = Limit(10)
	assert.NotNil(t, s)

	// 测试 Offset
	s = Offset(20)
	assert.NotNil(t, s)

	// 测试组合 Scope
	combined := WithReadReplica(Unscoped(), Limit(10))
	assert.NotNil(t, combined)

	// 测试 Page 转换
	s = WithFullPagination(1, 20, "-id")
	assert.NotNil(t, s)
}

func TestConvertPage(t *testing.T) {
	order, limit, offset := convertPage(1, 50, "age DESC")
	assert.Equal(t, "age DESC", order)
	assert.Equal(t, 50, limit)
	assert.Equal(t, 50, offset)

	order, limit, offset = convertPage(-1, 20, "id")
	assert.Equal(t, 0, offset)

	order, limit, offset = convertPage(0, 2000, "id")
	assert.Equal(t, 1000, limit) // 被限制为最大值
}

func TestWithDefaultPagination(t *testing.T) {
	s := WithDefaultPagination(2)
	assert.NotNil(t, s)
	// 分页计算逻辑已在 TestConvertPage 中覆盖，此处不再重复执行 Scope
}
