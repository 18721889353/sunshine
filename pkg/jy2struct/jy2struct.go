package jy2struct

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// ForceFloats whether to force a change to float
var ForceFloats bool

// commonInitialisms is a set of common initialisms.
// Only add entries that are highly unlikely to be non-initialisms.
// For instance, "ID" is fine (Freudian code is rare), but "AND" is not.
var commonInitialisms = map[string]bool{
	"API":   true,
	"ASCII": true,
	"CPU":   true,
	"CSS":   true,
	"DNS":   true,
	"EOF":   true,
	"GUID":  true,
	"HTML":  true,
	"HTTP":  true,
	"HTTPS": true,
	"ID":    true,
	"IP":    true,
	"JSON":  true,
	"LHS":   true,
	"QPS":   true,
	"RAM":   true,
	"RHS":   true,
	"RPC":   true,
	"SLA":   true,
	"SMTP":  true,
	"SSH":   true,
	"TLS":   true,
	"TTL":   true,
	"UI":    true,
	"UID":   true,
	"UUID":  true,
	"URI":   true,
	"URL":   true,
	"UTF8":  true,
	"VM":    true,
	"XML":   true,
	"NTP":   true,
	"DB":    true,
}

var intToWordMap = []string{
	"zero",
	"one",
	"two",
	"three",
	"four",
	"five",
	"six",
	"seven",
	"eight",
	"nine",
}

// Parser parser function
type Parser func(io.Reader) (interface{}, error)

// ParseJSON parse json to struct
func ParseJSON(input io.Reader) (interface{}, error) {
	var result interface{}
	if err := json.NewDecoder(input).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ParseYaml parse yaml to struct
func ParseYaml(input io.Reader) (interface{}, error) {
	var result interface{}
	b, err := readFile(input)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func readFile(input io.Reader) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	_, err := io.Copy(buf, input)
	if err != nil {
		return []byte{}, nil
	}
	return buf.Bytes(), nil
}

// json or yaml parse
func jyParse(input io.Reader, parser Parser, structName, pkgName string, tags []string, subStruct bool, convertFloats bool) ([]byte, error) {
	_ = pkgName
	var subStructMap map[string]string
	if subStruct {
		subStructMap = make(map[string]string)
	}

	var result map[string]interface{}

	iresult, err := parser(input)
	if err != nil {
		return nil, err
	}

	switch iresult := iresult.(type) {
	case map[interface{}]interface{}:
		result = convertKeysToStrings(iresult)
	case map[string]interface{}:
		result = iresult
	case []interface{}:
		src := fmt.Sprintf("\ntype %s %s\n", structName, typeForValue(iresult, structName, tags, subStructMap, convertFloats))
		// supplementary sub-structures
		for k, v := range subStructMap {
			src += fmt.Sprintf("\n\ntype %s %s\n\n", v, k)
		}
		var formatted []byte
		formatted, err = format.Source([]byte(src))
		if err != nil {
			err = fmt.Errorf("error formatting: %s, was formatting\n%s", err, src)
		}
		return formatted, err
	default:
		return nil, fmt.Errorf("unexpected type: %T", iresult)
	}

	src := fmt.Sprintf("\ntype %s %s}", structName, generateTypes(result, structName, tags, 0, subStructMap, convertFloats))

	keys := make([]string, 0, len(subStructMap))
	for key := range subStructMap {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, k := range keys {
		src = fmt.Sprintf("%v\n\ntype %v %v", src, subStructMap[k], k)
	}

	formatted, err := format.Source([]byte(src))
	if err != nil {
		err = fmt.Errorf("error formatting: %s, was formatting\n%s", err, src)
	}
	return formatted, err
}

func convertKeysToStrings(obj map[interface{}]interface{}) map[string]interface{} {
	res := make(map[string]interface{})

	for k, v := range obj {
		res[fmt.Sprintf("%v", k)] = v
	}

	return res
}

// handleNestedValue 处理嵌套值类型并返回适当的类型字符串
// 返回值: (valueType, sub) - valueType是字段类型，sub是子结构定义（如果有）
func handleNestedValue(value interface{}, key string, structName string, tags []string, depth int, subStructMap map[string]string, convertFloats bool) (valueType string, sub string) {
	valueType = typeForValue(value, structName, tags, subStructMap, convertFloats)
	sub = ""

	switch v := value.(type) {
	case []interface{}:
		if len(v) > 0 {
			if firstElem, ok := v[0].(map[interface{}]interface{}); ok {
				sub = generateTypes(convertKeysToStrings(firstElem), structName, tags, depth+1, subStructMap, convertFloats) + "}"
			} else if firstElem, ok := v[0].(map[string]interface{}); ok {
				sub = generateTypes(firstElem, structName, tags, depth+1, subStructMap, convertFloats) + "}"
			}
		}
	case map[interface{}]interface{}:
		sub = generateTypes(convertKeysToStrings(v), structName, tags, depth+1, subStructMap, convertFloats) + "}"
	case map[string]interface{}:
		sub = generateTypes(v, structName, tags, depth+1, subStructMap, convertFloats) + "}"
	}

	if sub == "" {
		return valueType, ""
	}

	// 处理子结构命名
	subName := getSubStructName(sub, key, structName, subStructMap)
	if sub != "" {
		// 根据子结构更新valueType
		switch value.(type) {
		case []interface{}:
			valueType = "[]" + subName
		default:
			valueType = subName
		}
	}

	return valueType, sub
}

// getSubStructName 获取或创建子结构的名称
// 如果子结构已存在于subStructMap中，则返回已有名称；否则使用字段名生成新名称
func getSubStructName(sub string, key string, _ string, subStructMap map[string]string) string {
	if subStructMap == nil {
		return sub
	}

	if val, ok := subStructMap[sub]; ok {
		return val
	}

	subName := FmtFieldName(key) // 使用字段名作为子结构名称
	subStructMap[sub] = subName
	return subName
}

// buildFieldTag 构建字段的struct tag字符串
func buildFieldTag(key string, tags []string) string {
	tagList := make([]string, 0, len(tags))
	for _, t := range tags {
		tagList = append(tagList, fmt.Sprintf("%s:\"%s\"", t, key))
	}
	return strings.Join(tagList, " ")
}

// jyParse go struct entries for a map[string]interface{} structure
func generateTypes(obj map[string]interface{}, structName string, tags []string, depth int, subStructMap map[string]string, convertFloats bool) string {
	structure := "struct {"

	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := obj[key]
		valueType, _ := handleNestedValue(value, key, structName, tags, depth, subStructMap, convertFloats)

		fieldName := FmtFieldName(key)
		tagStr := buildFieldTag(key, tags)

		structure += fmt.Sprintf("\n%s %s `%s`",
			fieldName,
			valueType,
			tagStr)
	}
	return structure
}

// FmtFieldName formats a string as a struct key
//
// Example:
//
//	FmtFieldName("foo_id")
//
// Output: FooID
func FmtFieldName(s string) string {
	runes := []rune(s)
	for len(runes) > 0 && !unicode.IsLetter(runes[0]) && !unicode.IsDigit(runes[0]) {
		runes = runes[1:]
	}
	if len(runes) == 0 {
		return "_"
	}

	s = stringifyFirstChar(string(runes))
	name := lintFieldName(s)
	runes = []rune(name)
	for i, c := range runes {
		ok := unicode.IsLetter(c) || unicode.IsDigit(c)
		if i == 0 {
			ok = unicode.IsLetter(c)
		}
		if !ok {
			runes[i] = '_'
		}
	}
	s = string(runes)
	s = strings.Trim(s, "_")
	if len(s) == 0 {
		return "_"
	}
	return s
}

// nolint
func lintFieldName(name string) string {
	// Fast path for simple cases: "_" and all lowercase.
	if name == "_" {
		return name
	}

	allLower := true
	for _, r := range name {
		if !unicode.IsLower(r) {
			allLower = false
			break
		}
	}
	if allLower {
		runes := []rune(name)
		if u := strings.ToUpper(name); commonInitialisms[u] {
			copy(runes[0:], []rune(u))
		} else {
			runes[0] = unicode.ToUpper(runes[0])
		}
		return string(runes)
	}

	allUpperWithUnderscore := true
	for _, r := range name {
		if !unicode.IsUpper(r) && r != '_' {
			allUpperWithUnderscore = false
			break
		}
	}
	if allUpperWithUnderscore {
		name = strings.ToLower(name)
	}

	// Split camelCase at any lower->upper transition, and split on underscores.
	// Check each word for common initialisms.
	runes := []rune(name)
	w, i := 0, 0 // index of start of word, scan
	for i+1 <= len(runes) {
		eow := false // whether we hit the end of a word

		if i+1 == len(runes) {
			eow = true
		} else if runes[i+1] == '_' {
			// underscore; shift the remainder forward over any run of underscores
			eow = true
			n := 1
			for i+n+1 < len(runes) && runes[i+n+1] == '_' {
				n++
			}

			// Leave at most one underscore if the underscore is between two digits
			if i+n+1 < len(runes) && unicode.IsDigit(runes[i]) && unicode.IsDigit(runes[i+n+1]) {
				n--
			}

			copy(runes[i+1:], runes[i+n+1:])
			runes = runes[:len(runes)-n]
		} else if unicode.IsLower(runes[i]) && !unicode.IsLower(runes[i+1]) {
			// lower->non-lower
			eow = true
		}
		i++
		if !eow {
			continue
		}

		// [w,i) is a word.
		word := string(runes[w:i])
		if u := strings.ToUpper(word); commonInitialisms[u] {
			// All the common initialisms are ASCII,
			// so we can replace the bytes exactly.
			copy(runes[w:], []rune(u))
		} else if strings.ToLower(word) == word {
			// already all lowercase, and not the first word, so uppercase the first character.
			runes[w] = unicode.ToUpper(runes[w])
		}
		w = i
	}
	return string(runes)
}

// generate an appropriate struct type entry
func typeForValue(value interface{}, structName string, tags []string, subStructMap map[string]string, convertFloats bool) string {
	//Check if this is an array
	if objects, ok := value.([]interface{}); ok {
		types := make(map[reflect.Type]bool, 0)
		for _, o := range objects {
			types[reflect.TypeOf(o)] = true
		}
		if len(types) == 1 {
			merged := mergeElements(objects).([]interface{}) //nolint:errcheck
			return "[]" + typeForValue(merged[0], structName, tags, subStructMap, convertFloats)
		}
		return "[]interface{}"
	} else if object, ok := value.(map[interface{}]interface{}); ok {
		return generateTypes(convertKeysToStrings(object), structName, tags, 0, subStructMap, convertFloats) + "}"
	} else if object, ok := value.(map[string]interface{}); ok {
		return generateTypes(object, structName, tags, 0, subStructMap, convertFloats) + "}"
	} else if reflect.TypeOf(value) == nil {
		return "interface{}"
	}
	v := reflect.TypeOf(value).Name()
	if v == "float64" && convertFloats {
		v = disambiguateFloatInt(value)
	}
	return v
}

// All numbers will initially be read as float64
// If the number appears to be an integer value, use int instead
func disambiguateFloatInt(value interface{}) string {
	const epsilon = .0001
	vfloat, ok := value.(float64)
	if !ok {
		return reflect.TypeOf(value).Name()
	}
	if !ForceFloats && math.Abs(vfloat-math.Floor(vfloat+epsilon)) < epsilon {
		var tmp int64
		return reflect.TypeOf(tmp).Name()
	}
	return reflect.TypeOf(value).Name()
}

// convert first character ints to strings
func stringifyFirstChar(str string) string {
	first := str[:1]

	i, err := strconv.ParseInt(first, 10, 8)
	if err != nil {
		return str
	}

	return intToWordMap[i] + "_" + str[1:]
}

func mergeElements(i interface{}) interface{} {
	switch i := i.(type) {
	default:
		return i
	case []interface{}:
		l := len(i)
		if l == 0 {
			return i
		}
		for j := 1; j < l; j++ {
			i[0] = mergeObjects(i[0], i[j])
		}
		return i[0:1]
	}
}

func mergeObjects(o1, o2 interface{}) interface{} {
	if o1 == nil {
		return o2
	}

	if o2 == nil {
		return o1
	}

	if reflect.TypeOf(o1) != reflect.TypeOf(o2) {
		return nil
	}

	switch i := o1.(type) {
	default:
		return o1
	case []interface{}:
		if i2, ok := o2.([]interface{}); ok {
			i3 := append(i, i2...)
			return mergeElements(i3)
		}
		return mergeElements(i)
	case map[string]interface{}:
		if i2, ok := o2.(map[string]interface{}); ok {
			for k, v := range i2 {
				if v2, ok := i[k]; ok {
					i[k] = mergeObjects(v2, v)
				} else {
					i[k] = v
				}
			}
		}
		return i
	case map[interface{}]interface{}:
		if i2, ok := o2.(map[interface{}]interface{}); ok {
			for k, v := range i2 {
				if v2, ok := i[k]; ok {
					i[k] = mergeObjects(v2, v)
				} else {
					i[k] = v
				}
			}
		}
		return i
	}
}
