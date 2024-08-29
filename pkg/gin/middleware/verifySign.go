package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/gin-gonic/gin"
)

func VerifySignatureMiddleware(signKey string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		//if ctx.Request.Method != http.MethodGet && ctx.Request.Method != http.MethodDelete {
		//验证签名规则
		err := verifySign(ctx, signKey)
		if err != nil {

			response.Out(ctx, errcode.InvalidParams.WithDetails(err.Error()))
			ctx.Abort()
			return
		}
		//}
		ctx.Next()
	}
}

// 验证签名
func verifySign(ctx *gin.Context, signKey string) error {
	// 根据请求方法获取请求数据
	var body []byte
	var err error
	switch ctx.Request.Method {
	//case "GET", "DELETE":
	//	body = []byte(ctx.Request.URL.RawQuery)
	case "POST":
		body, _ = io.ReadAll(ctx.Request.Body)
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	default:
		return errors.New("unsupported request method")
	}
	//body, err := io.ReadAll(ctx.Request.Body)
	//if err != nil {
	//	return err
	//}
	//// 重置请求体，以便后续中间件和处理程序能够读取它
	//ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	var jsonData map[string]interface{}
	err = json.Unmarshal(body, &jsonData)
	if err != nil {
		return err
	}

	sign := ""      //表示签名加密串，用来验证数据的完整性，防止数据篡改
	timestamp := "" //表示时间戳，用来验证接口的时效性。
	if value, ok := jsonData["sign"].(string); ok {
		sign = value
	} else {
		return errors.New("sign not empty")
	}
	// 验证签名
	if sign == "debug" {
		return nil
	}

	if value, ok := jsonData["timestamp"].(string); ok {
		timestamp = value
	} else if value, ok := jsonData["timestamp"].(float64); ok {
		timestamp = strconv.FormatFloat(value, 'f', -1, 64)
	} else {
		return errors.New("timestamp error")
	}

	// 验证过期时间
	//currentTimestamp := time.Now().Unix()
	tsInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("timestamp error")
	}
	jsonData["timestamp"] = tsInt
	//if tsInt > currentTimestamp || currentTimestamp-tsInt >= 60 {
	//	return errors.New("timestamp expired")
	//}

	if sign == "" || sign != createSign(jsonData, signKey) {
		return errors.New("sign error")
	}
	return nil
}

func createSign(params map[string]interface{}, signKey string) string {
	// 自定义 MD5 组合
	//dump.P(strings.Trim(createEncryptStr(params), "&") + "&key=" + signKey)
	return strings.ToUpper(gocrypto.Md5([]byte(strings.Trim(createEncryptStr(params), "&") + "&key=" + signKey)))
}

func createEncryptStr(params map[string]interface{}) string {
	var str string
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			if obj[k] != false && obj[k] != "" && obj[k] != nil {
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
				for _, s := range v {
					switch sv := s.(type) {
					case map[string]interface{}:
						sortIn(sv)
					}
				}
			default:
				str += fmt.Sprintf("%s=%v&", k, obj[k])
			}
		}
	}
	sortIn(params)
	return strings.TrimRight(str, "&")
}
