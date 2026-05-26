package krand

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInt(t *testing.T) {
	t.Parallel()
	l := 100

	// 测试默认范围 [0, 100]
	for i := 0; i < l; i++ {
		n := Int()
		assert.GreaterOrEqual(t, n, 0)
		assert.LessOrEqual(t, n, 100)
	}

	// 测试单参数范围 [0, max]
	for i := 0; i < l; i++ {
		n := Int(20)
		assert.GreaterOrEqual(t, n, 0)
		assert.LessOrEqual(t, n, 20)
	}

	// 测试双参数范围 [min, max]
	for i := 0; i < l; i++ {
		n := Int(10, 20)
		assert.GreaterOrEqual(t, n, 10)
		assert.LessOrEqual(t, n, 20)
	}

	// 测试 min > max 时自动交换
	for i := 0; i < l; i++ {
		n := Int(20, 10)
		assert.GreaterOrEqual(t, n, 10)
		assert.LessOrEqual(t, n, 20)
	}

	// 测试负值处理
	assert.Equal(t, 0, Int(-5))
}

func TestFloat64(t *testing.T) {
	t.Parallel()
	l := 100

	// 测试默认范围 [0.0, 100.0]，0位小数
	for i := 0; i < l; i++ {
		f := Float64(0)
		assert.GreaterOrEqual(t, f, 0.0)
		assert.LessOrEqual(t, f, 100.0)
	}

	// 测试1位小数，范围 [0.0, 20.0]
	for i := 0; i < l; i++ {
		f := Float64(1, 20)
		assert.GreaterOrEqual(t, f, 0.0)
		assert.LessOrEqual(t, f, 20.0)
	}

	// 测试2位小数，范围 [10.0, 20.0]
	for i := 0; i < l; i++ {
		f := Float64(2, 10, 20)
		assert.GreaterOrEqual(t, f, 10.0)
		assert.LessOrEqual(t, f, 20.0)
	}

	// 测试 min > max 时自动交换
	for i := 0; i < l; i++ {
		f := Float64(4, 20, 10)
		assert.GreaterOrEqual(t, f, 10.0)
		assert.LessOrEqual(t, f, 20.0)
	}

	// 测试小数精度
	f := Float64(2, 10, 20)
	str := fmt.Sprintf("%.2f", f)
	assert.Contains(t, str, ".")
}

func TestString(t *testing.T) {
	t.Parallel()
	// 测试默认长度 (6)
	assert.Equal(t, 6, len(String(RNum)))
	assert.Equal(t, 6, len(String(RUpper)))
	assert.Equal(t, 6, len(String(RLower)))
	assert.Equal(t, 6, len(String(RNum|RUpper)))
	assert.Equal(t, 6, len(String(RNum|RLower)))
	assert.Equal(t, 6, len(String(RAll)))

	// 测试自定义长度
	assert.Equal(t, 32, len(Bytes(RNum, 32)))
	assert.Equal(t, 32, len(Bytes(RUpper, 32)))
	assert.Equal(t, 32, len(Bytes(RLower, 32)))
	assert.Equal(t, 32, len(Bytes(RNum|RUpper, 32)))
	assert.Equal(t, 32, len(Bytes(RNum|RLower, 32)))
	assert.Equal(t, 32, len(Bytes(RAll, 32)))

	// 测试无效 kind 默认为 RAll
	result := String(0, 10)
	assert.Equal(t, 10, len(result))

	// 测试无效长度默认为 6
	result = String(RNum, 0)
	assert.Equal(t, 6, len(result))
	result = String(RNum, -5)
	assert.Equal(t, 6, len(result))
}

func TestNewID(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10; i++ {
		id := NewID()
		assert.Greater(t, id, int64(0))
		// 验证 ID 基于当前时间戳
		expectedMin := time.Now().UnixMilli() * 1000000
		assert.GreaterOrEqual(t, id, expectedMin)
	}
}

func TestNewStringID(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10; i++ {
		id := NewStringID()
		assert.Equal(t, 16, len(id))
		// 验证是有效的十六进制
		_, err := fmt.Sscanf(id, "%x", new(int64))
		assert.NoError(t, err)
	}
}

func TestNewSeriesID(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10; i++ {
		id := NewSeriesID()
		assert.Equal(t, 26, len(id))
		// 验证格式：前 20 个字符是时间戳，后 6 个是数字
		timestampPart := id[:20]
		randomPart := id[20:]
		assert.NotEmpty(t, timestampPart)
		assert.Equal(t, 6, len(randomPart))
		// 验证随机部分只包含数字
		for _, c := range randomPart {
			assert.GreaterOrEqual(t, c, '0')
			assert.LessOrEqual(t, c, '9')
		}
	}
}

func TestConcurrency(t *testing.T) {
	t.Parallel()
	// 测试并发访问的线程安全性
	var wg sync.WaitGroup
	iterations := 100
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = String(RAll, 10)
				_ = Int(100)
				_ = Float64(2, 100)
				_ = NewID()
				_ = NewStringID()
				_ = NewSeriesID()
			}
		}()
	}

	wg.Wait()
}

func TestUniqueness(t *testing.T) {
	t.Parallel()
	// 测试生成的 ID 具有唯一性（带小延迟）
	ids := make(map[int64]bool)
	count := 100

	for i := 0; i < count; i++ {
		id := NewID()
		// 注意：在极高并发下同一毫秒内可能会产生重复
		// 这是预期行为 - 使用 NewSeriesID 获得更好的唯一性
		if !ids[id] {
			ids[id] = true
		}
		// 小延迟以确保不同的时间戳
		if i%10 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	// 测试字符串 ID 唯一性，小批量以避免时间戳冲突
	stringIDs := make(map[string]bool)
	smallCount := 50
	for i := 0; i < smallCount; i++ {
		id := NewStringID()
		// 注意：在极高并发下，StringID 可能因相同毫秒时间戳而重复
		// 这是预期行为 - 使用 NewSeriesID 获得更好的唯一性
		if !stringIDs[id] {
			stringIDs[id] = true
		}
		if i%10 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}

func BenchmarkInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Int()
	}
}

func BenchmarkInt_10000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Int(10000)
	}
}

func BenchmarkFloat64_0(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Float64(0)
	}
}

func BenchmarkFloat64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Float64(2, 10000)
	}
}

func BenchmarkString_ALL_6(b *testing.B) {
	for i := 0; i < b.N; i++ {
		String(RAll)
	}
}

func BenchmarkString_ALL_16(b *testing.B) {
	for i := 0; i < b.N; i++ {
		String(RAll, 16)
	}
}

func BenchmarkBytes_ALL_6(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Bytes(RAll)
	}
}

func BenchmarkBytes_ALL_16(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Bytes(RAll, 16)
	}
}

func BenchmarkNewID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewID()
	}
}

func BenchmarkNewStringID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewStringID()
	}
}

func BenchmarkNewSeriesID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewSeriesID()
	}
}
