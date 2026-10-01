package gogroutine

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 本文件是 gogroutine 的模糊测试（Fuzz test），把「不变量」交给随机输入反复冲击：
// 池容量裁剪永不越界、nil/取消 ctx 归一与跳过判定不 panic 且与 Err() 一致、
// 批量错误聚合不丢错误。
//
// 运行方式：
//
//	# 只跑内置种子语料（默认随 go test 执行，耗时可忽略）
//	go test -run='^Fuzz' ./pkg/gogroutine/
//	# 真正的模糊挖掘（不进 CI 常规流程，按需本地跑）
//	go test -fuzz=FuzzClampPoolSize      -fuzztime=30s ./pkg/gogroutine/
//	go test -fuzz=FuzzNormalizeCtx       -fuzztime=30s ./pkg/gogroutine/
//	go test -fuzz=FuzzCollectBatchErrors -fuzztime=30s ./pkg/gogroutine/
//
// 失败语料会写入 testdata/fuzz/<TargetName>/，需要人工确认后提交或修正。

// FuzzClampPoolSize 验证容量裁剪的不变量：
//  1. 任意输入不 panic；
//  2. 结果恒落在 [MinPoolSize, MaxPoolSize]（越界会把 ants.NewPool 打到非法容量）；
//  3. 幂等——裁剪两次等于裁剪一次（重复 apply 同一 Option 不漂移）。
func FuzzClampPoolSize(f *testing.F) {
	for _, seed := range []int{
		0, 1, MinPoolSize - 1, MinPoolSize, MinPoolSize + 1,
		DefaultPoolSize, MaxPoolSize - 1, MaxPoolSize, MaxPoolSize + 1,
		-1, -1 << 31, 1<<31 - 1,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, size int) {
		got := clampPoolSize(size)
		if got < MinPoolSize || got > MaxPoolSize {
			t.Fatalf("clampPoolSize(%d) = %d, 越界 [%d, %d]", size, got, MinPoolSize, MaxPoolSize)
		}
		if again := clampPoolSize(got); again != got {
			t.Fatalf("裁剪不幂等: clamp(%d) = %d != %d", got, again, got)
		}
	})
}

// FuzzNormalizeCtx 验证 ctx 归一与跳过判定的不变量：
//  1. 任意输入不 panic（原实现在 nil 接口上调 ctx.Done() 直接 panic）；
//  2. 归一结果恒非 nil，nil 输入归一为 context.Background()；
//  3. shouldSkipSubmit 与 ctx.Err() 判定一致——不会出现「已取消仍入队」或「未取消被误杀」。
func FuzzNormalizeCtx(f *testing.F) {
	// 种子按长度覆盖全部 5 个分支：0→bg、1→已取消、2→截止已过、3→超时已到期、4→nil
	for _, seed := range []string{"", "a", "ab", "abc", "abcd"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, variant string) {
		var ctx context.Context
		switch len(variant) % 5 {
		case 0: // 未取消
			ctx = context.Background()
		case 1: // 已取消
			c, cancel := context.WithCancel(context.Background())
			cancel()
			ctx = c
		case 2: // 截止时间已过
			c, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			ctx = c
		case 3: // 短超时后已到期
			c, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
			defer cancel()
			time.Sleep(time.Millisecond)
			ctx = c
		default: // nil
			ctx = nil
		}

		normalized := normalizeCtx(ctx)
		if normalized == nil {
			t.Fatal("normalizeCtx 不得返回 nil")
		}
		if ctx == nil && normalized != context.Background() {
			t.Fatal("nil ctx 必须归一为 context.Background()")
		}

		// 跳过判定与 Err() 一致（nil ctx 语义等价 Background，即未取消）
		want := false
		if normalized.Err() != nil {
			want = true
		}
		if got := shouldSkipSubmit(ctx, "fuzz"); got != want {
			t.Fatalf("shouldSkipSubmit(%v) = %v, want %v（必须与 Err() 一致）", ctx, got, want)
		}
	})
}

// FuzzCollectBatchErrors 验证批量错误聚合的不变量：
//  1. 任意输入不 panic；
//  2. 全 nil（含空切片）聚合结果为 nil——调用方据 err==nil 判定整批成功；
//  3. 只要存在非 nil 错误，结果必非 nil 且 errors.Is 能命中每一个（errors.Join 不丢错）。
func FuzzCollectBatchErrors(f *testing.F) {
	f.Add(0, int32(0))
	f.Add(4, int32(0b1010))
	f.Add(8, int32(0b11111111))
	f.Add(1, int32(1))

	f.Fuzz(func(t *testing.T, n int, mask int32) {
		if n < 0 || n > 64 {
			t.Skip() // 只关心 0~64 规模的切片
		}
		errs := make([]error, n)
		wantErrs := make([]error, 0, n)
		for i := 0; i < n; i++ {
			if mask&(1<<uint(i)) != 0 {
				e := errors.New("fuzz-err")
				errs[i] = e
				wantErrs = append(wantErrs, e)
			}
		}

		got := collectBatchErrors(errs)
		if len(wantErrs) == 0 {
			if got != nil {
				t.Fatalf("全 nil 输入聚合结果 = %v, want nil", got)
			}
			return
		}
		if got == nil {
			t.Fatalf("存在 %d 个错误却聚合为 nil", len(wantErrs))
		}
		for i, e := range wantErrs {
			if !errors.Is(got, e) {
				t.Fatalf("聚合结果丢失第 %d 个错误: %v", i, got)
			}
		}
	})
}
