package utils

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"io"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// Hmac 计算hmac
func Hmac(s string, appSecret string) string {
	h := hmac.New(sha256.New, []byte(appSecret))
	_, _ = h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func Md5(s string) string {
	h := md5.New()
	_, _ = io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}

// Marshal 序列化参数
func Marshal(o interface{}, appSecret string, signFunc func(params map[string]any, appSecret string) string) string {
	// 序列化一次
	raw, _ := json.Marshal(o)

	// 反序列化为map
	m := make(map[string]interface{})
	reader := bytes.NewReader(raw)
	decode := json.NewDecoder(reader)
	decode.UseNumber()
	_ = decode.Decode(&m)
	m["timestamp"] = time.Now().Unix()
	m["nonce_str"] = RandStringBytesMaskImprSrcUnsafe(32)

	// 使用传入的签名函数或者默认签名函数
	if signFunc != nil {
		m["sign"] = signFunc(m, appSecret)
	} else {
		m["sign"] = Sign(m, appSecret)
	}
	// 重新做一次序列化，并禁用Html Escape
	buffer := bytes.NewBufferString("")
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(m)

	marshal := strings.TrimSpace(buffer.String()) // Trim掉末尾的换行符
	return marshal
}

func RandStringBytesMaskImprSrcUnsafe(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const (
		letterIdxBits = 6                    // 6 bits to represent a letter index
		letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
		letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
	)
	src := rand.NewSource(time.Now().UnixNano())
	b := make([]byte, n)
	v := src.Int63()
	for i, cache, remain := 0, v, letterIdxMax; i < n; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i++
		}
		cache >>= letterIdxBits
		remain--
	}
	return string(b)
}

func Sign(params map[string]interface{}, signKey string) string {
	key := strings.Trim(createEncryptStr(params), "&")
	key = key + "&key=" + signKey
	// 自定义 MD5 组合
	return strings.ToUpper(gocrypto.Md5([]byte(key)))
}
func createEncryptStr(params map[string]interface{}) string {
	var strBuilder strings.Builder
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		keys := make([]string, 0, len(obj))
		for k, v := range obj {
			if v != "" && v != nil {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "sign" {
				continue
			}
			switch v := obj[k].(type) {
			case map[string]interface{}:
				sortIn(v)
			case []interface{}:
				for idx, s := range v {
					switch sv := s.(type) {
					case map[string]interface{}:
						sortIn(sv) // 递归处理子 map
					default:
						strBuilder.WriteString(fmt.Sprintf("%d=%v&", idx, sv)) // 修改点：index=value 拼接
					}
				}
			default:
				strBuilder.WriteString(fmt.Sprintf("%s=%v&", k, v))
			}
		}
	}
	sortIn(params)
	result := strBuilder.String()
	if len(result) > 0 {
		result = result[:len(result)-1] // Remove the trailing '&'
	}
	return result
}
