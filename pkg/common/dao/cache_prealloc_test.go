package dao

import (
	"reflect"
	"testing"
)

// TestFindMissedIDs_PreallocBehavior findMissedIDs 预分配后的行为守护：
// 已有 ID 不进 missed、缺失 ID 保序返回、空输入返回空。
func TestFindMissedIDs_PreallocBehavior(t *testing.T) {
	m := &cacheManager[testPreallocObj]{}
	items := map[uint64]*testPreallocObj{
		1: {ID: 1}, 3: {ID: 3}, 5: {ID: 5},
	}

	tests := []struct {
		name string
		ids  []uint64
		want []uint64
	}{
		{"全部命中", []uint64{1, 3, 5}, []uint64{}},
		{"全部缺失", []uint64{2, 4}, []uint64{2, 4}},
		{"混合保序", []uint64{9, 1, 8, 3, 7}, []uint64{9, 8, 7}},
		{"空输入", nil, []uint64{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.findMissedIDs(tt.ids, items)
			if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("findMissedIDs(%v) = %v, want %v", tt.ids, got, tt.want)
			}
		})
	}
}

type testPreallocObj struct{ ID uint64 }
