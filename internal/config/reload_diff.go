package config

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/18721889353/sunshine/pkg/logger"
)

// logConfigDiff 配置变更审计日志：按顶层配置段对比新旧配置，输出字段级变更描述。
// 与各回调日志互补：回调日志展示"热更做了什么动作"，此处展示"配置改了哪些值、改前改后是什么"，
// 让运维从一次推送的日志中直接看到具体变更数据（此前多数回调只打新值，看不到旧值）。
// 敏感字段（DSN/密码/密钥等）值脱敏输出。
func logConfigDiff(oldCfg, newCfg *Config) {
	// 1. 空配置直接跳过
	if oldCfg == nil || newCfg == nil {
		return
	}

	ctx := context.Background()
	oldVal := reflect.ValueOf(oldCfg).Elem()
	newVal := reflect.ValueOf(newCfg).Elem()

	// 2. 逐顶层配置段对比，有差异的段各输出一条日志（无差异的段静默）
	typ := oldVal.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		section := configFieldName(field)
		if section == "" {
			continue
		}
		var parts []string
		diffConfigValue(section, oldVal.Field(i), newVal.Field(i), &parts)
		if len(parts) == 0 {
			continue
		}
		logger.InfoWithCtx(ctx, "[config reload] 配置变更",
			logger.String("section", section),
			logger.String("change", strings.Join(parts, ", ")),
		)
	}
}

// diffConfigValue 递归对比新旧配置值，将差异以 "路径:旧值->新值" 追加到 parts。
// 处理策略：struct 逐导出字段下钻（字段名取 yaml tag，与 yml 配置对应）；
// slice 按索引对比；map[string]X 按 key 对比；其余类型作为叶子格式化比较。
func diffConfigValue(path string, oldVal, newVal reflect.Value, parts *[]string) {
	// 1. 值相等则无差异（配置为纯数据结构，DeepEqual 足够）
	if reflect.DeepEqual(oldVal.Interface(), newVal.Interface()) {
		return
	}

	// 2. 指针：两侧均非空时解引用下钻；任一为空按叶子整体输出（nil<->非nil 场景）
	for oldVal.Kind() == reflect.Pointer && newVal.Kind() == reflect.Pointer &&
		!oldVal.IsNil() && !newVal.IsNil() {
		oldVal, newVal = oldVal.Elem(), newVal.Elem()
	}

	switch oldVal.Kind() {
	// 3. struct：逐导出字段递归
	case reflect.Struct:
		if newVal.Kind() != reflect.Struct {
			appendConfigLeaf(path, oldVal, newVal, parts)
			return
		}
		typ := oldVal.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			name := configFieldName(field)
			if name == "" {
				continue
			}
			diffConfigValue(path+"."+name, oldVal.Field(i), newVal.Field(i), parts)
		}
	// 4. slice/array：长度变化直接说明，共有索引逐一对比
	case reflect.Slice, reflect.Array:
		if newVal.Kind() != oldVal.Kind() {
			appendConfigLeaf(path, oldVal, newVal, parts)
			return
		}
		if oldVal.Len() != newVal.Len() {
			*parts = append(*parts, fmt.Sprintf("%s.length:%d->%d", path, oldVal.Len(), newVal.Len()))
		}
		n := oldVal.Len()
		if newVal.Len() < n {
			n = newVal.Len()
		}
		for i := 0; i < n; i++ {
			diffConfigValue(fmt.Sprintf("%s[%d]", path, i), oldVal.Index(i), newVal.Index(i), parts)
		}
	// 5. map：按键对比增删改
	case reflect.Map:
		diffConfigMap(path, oldVal, newVal, parts)
	// 6. 叶子：格式化输出 旧值->新值
	default:
		appendConfigLeaf(path, oldVal, newVal, parts)
	}
}

// diffConfigMap 对比 map 型配置：按两侧 key 集合的并集逐一比对，输出新增/删除/修改。
// 非 string key 的 map 无法稳定输出路径，退化为叶子整体对比。
func diffConfigMap(path string, oldVal, newVal reflect.Value, parts *[]string) {
	// 1. 非 string key 或类型不一致时退化为叶子输出
	if oldVal.Type().Key().Kind() != reflect.String || newVal.Type().Key().Kind() != reflect.String ||
		oldVal.Type() != newVal.Type() {
		appendConfigLeaf(path, oldVal, newVal, parts)
		return
	}

	// 2. 合并两侧 key 集合（排序保证日志输出稳定）
	keySet := make(map[string]struct{}, oldVal.Len()+newVal.Len())
	for _, m := range []reflect.Value{oldVal, newVal} {
		for iter := m.MapRange(); iter.Next(); {
			keySet[iter.Key().String()] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 3. 逐 key 对比：新增/删除输出整体值，共有 key 递归下钻
	for _, k := range keys {
		oldItem := oldVal.MapIndex(reflect.ValueOf(k))
		newItem := newVal.MapIndex(reflect.ValueOf(k))
		switch {
		case !oldItem.IsValid():
			*parts = append(*parts, fmt.Sprintf("%s.%s: (未配置)->%s", path, k, formatConfigValue(newItem)))
		case !newItem.IsValid():
			*parts = append(*parts, fmt.Sprintf("%s.%s: %s->(已删除)", path, k, formatConfigValue(oldItem)))
		default:
			diffConfigValue(path+"."+k, oldItem, newItem, parts)
		}
	}
}

// appendConfigLeaf 叶子值差异：格式化输出 "路径:旧值->新值"，敏感字段值脱敏。
func appendConfigLeaf(path string, oldVal, newVal reflect.Value, parts *[]string) {
	oldStr, newStr := formatConfigValue(oldVal), formatConfigValue(newVal)
	// 敏感字段（DSN/密码/密钥）不输出明文
	if isSensitiveConfigField(path) {
		oldStr, newStr = "******", "******"
	}
	*parts = append(*parts, fmt.Sprintf("%s:%s->%s", path, oldStr, newStr))
}

// formatConfigValue 格式化配置值用于日志输出：string 加引号，超长内容截断防止日志爆量。
func formatConfigValue(v reflect.Value) string {
	// 1. 无效值（如缺失的 map 项）输出占位符
	if !v.IsValid() {
		return "(空)"
	}

	// 2. 按类型格式化
	var s string
	switch v.Kind() {
	case reflect.String:
		s = fmt.Sprintf("%q", v.String())
	default:
		s = fmt.Sprintf("%v", v.Interface())
	}

	// 3. 超长截断（如 DSN、长列表），保留可辨识前缀
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "...(截断)"
	}
	return s
}

// isSensitiveConfigField 判断配置路径是否属于敏感字段（DSN/密码/密钥），值需脱敏输出。
// password/pwd/dsn/secret 用 contains 匹配派生名（如 mastersDsn/secretKey/accessKeySecret），
// signKey/appKey 按段名精确匹配；
// 注意不能对 "token" 做 contains——tokenCalculateStrategy 等业务字段会被误伤。
func isSensitiveConfigField(path string) bool {
	lower := strings.ToLower(path)

	// 1. 敏感关键词 contains 匹配
	for _, keyword := range []string{"password", "pwd", "dsn", "secret"} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	// 2. 段名精确匹配（取路径最后一段）
	last := lower
	if idx := strings.LastIndex(lower, "."); idx >= 0 {
		last = lower[idx+1:]
	}
	switch last {
	case "signkey", "appkey":
		return true
	}
	return false
}

// configFieldName 取结构体字段的配置名：优先 yaml tag（与 yml 配置对应），无 tag 则用字段名。
// 返回空串表示该字段不参与配置（yaml:"-"），调用方应跳过。
func configFieldName(field reflect.StructField) string {
	if tag := field.Tag.Get("yaml"); tag != "" {
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	return field.Name
}
