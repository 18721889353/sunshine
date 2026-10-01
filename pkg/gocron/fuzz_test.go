package gocron

import (
	"testing"

	"github.com/robfig/cron/v3"
)

// FuzzParseKVs 验证 parseKVs 的不变量：
// 1. 对任意输入不 panic；2. 奇数参数对返回 nil；3. 输出字段数不超过输入对数的一半；
// 4. EntryID → 任务名还原分支（key == "entry"）在偶数路径上每次执行必被命中。
//
// 第 4 条不能只靠随机生成 "entry" 键（单字符键永远碰不到 5 字符键名），
// 故偶数路径固定追加已映射的 entry 对，见下方 buildKVs 注释。
func FuzzParseKVs(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3})
	f.Add([]byte{5, 'k', 2})        // 非字符串键 + EntryID 值 + 奇数长度
	f.Add([]byte{'e', 1, 'j', 's'}) // 正常键值对
	f.Add([]byte{7, 1})             // 7%7==0 → 随机路径的 "entry" 键
	f.Add([]byte{7, 2})             // "entry" + EntryID 值

	f.Fuzz(func(t *testing.T, data []byte) {
		s := &Scheduler{}
		s.idName.Store(cron.EntryID(1), "seedTask") // 已映射分支：EntryID(1) → "seedTask"

		kvs := buildKVs(data)
		if len(kvs)%2 != 0 {
			// 奇数路径：必须返回 nil（此时追加 entry 对会破坏该分支，故单独断言后返回）
			if fields := s.parseKVs(kvs); fields != nil {
				t.Fatalf("奇数参数对应返回 nil，输入 %d 项，返回 %v", len(kvs), fields)
			}
			return
		}

		// 偶数路径：固定追加已映射的 entry 对，保证核心分支每次执行必被命中
		kvs = append(kvs, "entry", cron.EntryID(1))
		fields := s.parseKVs(kvs)
		if len(fields) > len(kvs)/2 {
			t.Fatalf("输出字段数 %d 超过输入对数上限 %d", len(fields), len(kvs)/2)
		}

		// 守护断言：entry 分支若未执行（或映射失效），此处必然找不到还原后的任务名
		found := false
		for _, fld := range fields {
			if fld.Key == "task" && fieldValue(fld) == "seedTask" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("EntryID → 任务名还原分支未被命中，fields=%v", fields)
		}
	})
}

// buildKVs 将字节流两两分组构造 kvs：
// 奇数位按取值构造字符串键（%7==0 时为特殊键 "entry"，否则单字符）、非字符串键（%5==0），
// 偶数位构造字符串值或 EntryID 值；总长为奇数时末尾只追加键，用于覆盖奇数参数对分支。
// 注意：随机路径的键碰运气最多只能生成 "entry"，核心分支覆盖由 fuzz body 固定追加保证，
// 此处的 %7 分支只是额外随机探索。
func buildKVs(data []byte) []any {
	kvs := make([]any, 0, len(data))
	for i := 0; i < len(data); i += 2 {
		switch {
		case data[i]%7 == 0:
			kvs = append(kvs, "entry") // 特殊键：随机路径也可命中 entry 分支
		case data[i]%5 == 0:
			kvs = append(kvs, int(data[i])) // 非字符串键
		default:
			kvs = append(kvs, string(rune(data[i])))
		}
		if i+1 >= len(data) {
			break
		}
		if data[i+1]%2 == 0 {
			kvs = append(kvs, cron.EntryID(data[i+1]))
		} else {
			kvs = append(kvs, string(rune(data[i+1])))
		}
	}
	return kvs
}

// FuzzNormalizeGranularity 验证 WithGranularity 的归一化不变量：
// 任意整数输入经选项应用后，粒度必须是 SecondType 或 MinuteType 之一。
func FuzzNormalizeGranularity(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(-1)
	f.Add(99)

	f.Fuzz(func(t *testing.T, granularity int) {
		o := defaultOptions()
		o.apply(WithGranularity(granularity))
		if o.granularity != SecondType && o.granularity != MinuteType {
			t.Fatalf("粒度必须归一化为 SecondType/MinuteType，实际 %d", o.granularity)
		}
	})
}
