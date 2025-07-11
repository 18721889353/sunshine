package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"
)

type XssOptions func(*xssOptions)

func defaultXssOptions() *xssOptions {
	return &xssOptions{
		ignoreUrls: map[string]struct{}{},
	}
}

type xssOptions struct {
	ignoreUrls map[string]struct{}
}

func (o *xssOptions) apply(opts ...XssOptions) {
	for _, opt := range opts {
		opt(o)
	}
}

func WithIgnoreXssUrl(urls ...string) XssOptions {
	return func(o *xssOptions) {
		for _, url := range urls {
			o.ignoreUrls[url] = struct{}{}
		}
	}
}

func XSSCrossMiddleware(opts ...XssOptions) gin.HandlerFunc {
	o := defaultXssOptions()
	o.apply(opts...)
	return func(ctx *gin.Context) {
		if _, ok := o.ignoreUrls[ctx.Request.URL.Path]; ok {
			ctx.Next()
			return
		}
		if err := xssCross(ctx); err != nil {
			response.Out(ctx, errcode.InvalidParams.WithOutMsg(err.Error()))
			ctx.Abort()
			return
		}
		// 继续处理请求
		ctx.Next()
	}
}

func xssCross(ctx *gin.Context) error {
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		return err
	}
	var jsonBody map[string]interface{}
	err = json.Unmarshal(body, &jsonBody)
	if err != nil {
		return err
	}
	policy := bluemonday.UGCPolicy()
	sanitizeJSON(jsonBody, policy)
	// 重置请求体，以便后续中间件和处理程序能够读取它
	marshal, err := json.Marshal(jsonBody)
	if err != nil {
		return err
	}

	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(marshal))
	return nil
}

// 递归遍历JSON对象，对其值进行XSS过滤
func sanitizeJSON(json map[string]interface{}, policy *bluemonday.Policy) {
	for key, value := range json {
		switch v := value.(type) {
		case string:
			cleaned := strings.TrimSpace(v)
			json[key] = policy.Sanitize(cleaned)
		case map[string]interface{}:
			sanitizeJSON(v, policy)
		case []interface{}:
			for i, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					sanitizeJSON(itemMap, policy)
					v[i] = itemMap
				} else if itemStr, ok := item.(string); ok {
					v[i] = policy.Sanitize(itemStr)
				}
			}
		}
	}
}
