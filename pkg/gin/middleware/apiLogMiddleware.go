package middleware

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
	"time"
)

// ResponseWriter 自定义响应writer，用于捕获响应体
type ResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write 实现Write方法，同时写入原始响应writer和缓冲区
func (rw *ResponseWriter) Write(data []byte) (int, error) {
	// 写入原始响应writer
	n, err := rw.ResponseWriter.Write(data)
	// 同时写入缓冲区以捕获响应体
	if n > 0 {
		rw.body.Write(data[:n])
	}
	return n, err
}

// APILogRecord 表示API日志记录的接口
type APILogRecord interface{}

// APILogFunc 定义记录API日志的函数类型
type APILogFunc func(c *gin.Context, reqBody []byte, respBody []byte, startTime time.Time, endTime time.Time, spendTime int64)

// APILogOptions API日志中间件配置选项
type APILogOptions struct {
	LogFunc APILogFunc
}

// APILogOption API日志中间件配置函数类型
type APILogOption func(*APILogOptions)

// WithAPILogFunc 设置自定义日志记录函数
func WithAPILogFunc(logFunc APILogFunc) APILogOption {
	return func(o *APILogOptions) {
		o.LogFunc = logFunc
	}
}

// APILogMiddleware 记录API请求日志的中间件
/**
使用案例
// 对象池用于重用CpDealerApiLog实例
var cpDealerApiLogPool = sync.Pool{
	New: func() interface{} {
		return &model.CpDealerApiLog{}
	},
}

func customLogFunc(c *gin.Context, reqBody []byte, respBody []byte, startTime time.Time, endTime time.Time, spendTime int64) {
	go func() {
		// 从池中获取CpDealerApiLog实例
		log := cpDealerApiLogPool.Get().(*model.CpDealerApiLog)
		// 重置字段值
		log.Type = "接口"
		log.Category = "API"
		log.IP = c.ClientIP()
		log.Url = c.Request.URL.String()
		log.Params = string(reqBody)
		log.Response = string(respBody)
		log.StartTime = cast.ToString(startTime.UnixMilli())
		log.EndTime = cast.ToString(endTime.UnixMilli())
		log.SpendTime = cast.ToString(spendTime)
		log.DealerID = c.GetInt("uid")
		log.Active = "golang api"
		log.CreateTime = cast.ToString(time.Now().Unix())
		log.UpdateTime = int(time.Now().Unix())

		// 保存到数据库
		database.GetDB().Create(log)

		// 使用完毕后将对象放回池中
		cpDealerApiLogPool.Put(log)
	}()
}

	r.Use(APILogMiddleware(WithApiLogFunc(customLogFunc)))

*/
func APILogMiddleware(opts ...APILogOption) gin.HandlerFunc {
	options := &APILogOptions{}

	for _, opt := range opts {
		opt(options)
	}

	return func(c *gin.Context) {
		// 记录开始时间
		startTime := time.Now()
		// 读取请求体
		var reqBody []byte
		if c.Request.Body != nil {
			reqBody, _ = c.GetRawData()
			c.Request.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		}
		// 创建自定义响应writer来捕获响应体
		responseWriter := &ResponseWriter{body: new(bytes.Buffer), ResponseWriter: c.Writer}
		c.Writer = responseWriter
		// 继续处理请求
		c.Next()
		// 记录结束时间
		endTime := time.Now()
		spendTime := endTime.Sub(startTime).Milliseconds()
		// 读取捕获的响应体
		respBody := responseWriter.body.Bytes()
		// 调用日志记录函数
		options.LogFunc(c, reqBody, respBody, startTime, endTime, spendTime)
	}
}
