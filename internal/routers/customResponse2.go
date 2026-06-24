package routers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/logger"
)

// CustomResponse2 represents a custom response handler version 2
type CustomResponse2 struct{}

// NewCustomResponse2 创建自定义响应处理器2
func NewCustomResponse2() *CustomResponse2 {
	return &CustomResponse2{}
}
func (r *CustomResponse2) response(c *gin.Context, code int, result, returnInfo interface{}) {
	if returnInfo == nil {
		returnInfo = make([]any, 0)
	}
	c.JSON(code, gin.H{
		"result":     result,
		"returnInfo": returnInfo,
	})
}

type dataInfo struct {
	Result     Result       `json:"result"`
	ReturnInfo []ReturnInfo `json:"returnInfo"`
}

// Result 结果信息结构
type Result struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	TransactionID   string `json:"transactionId"`
	TransactionTime string `json:"transactionTime"`
}

// ReturnInfo 返回信息结构
type ReturnInfo struct {
	OrderID    string `json:"orderId"`
	Msisdn     string `json:"msisdn"`
	SkuID      string `json:"skuId"`
	SkuName    string `json:"skuName"`
	OpenStatus int    `json:"openStatus"`
	OpenDesc   string `json:"openDesc"`
	OpenTime   string `json:"openTime"`
}

// Success handles successful response
func (r *CustomResponse2) Success(c *gin.Context, data interface{}) {
	var result = dataInfo{}
	err := copier.Copy(&result, data)
	if err != nil {
		ctx := middleware.WrapCtx(c)
		logger.WarnWithCtx(ctx, "copier.Copy error", logger.Err(err))
	}
	r.response(c, http.StatusOK, result.Result, result.ReturnInfo)
}

// Success2 handles successful response version 2
func (r *CustomResponse2) Success2(_ *gin.Context, _, _ string, _ interface{}) {
}

// ParamError handles parameter error response
func (r *CustomResponse2) ParamError(c *gin.Context, _ error) {
	r.response(c, http.StatusOK, nil, nil)
}

func (r *CustomResponse2) Error(c *gin.Context, err error) bool {
	st, _ := status.FromError(err)
	result := map[string]string{
		"code":            fmt.Sprintf("%d", st.Code()),
		"message":         st.Message(),
		"transactionId":   c.GetString("transactionId"),
		"transactionTime": time.Now().Format("20060102150405"),
	}
	r.response(c, http.StatusOK, result, nil)
	return false
}
