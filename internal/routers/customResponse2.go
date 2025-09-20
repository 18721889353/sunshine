package routers

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	"go.uber.org/zap"
	"google.golang.org/grpc/status"
	"net/http"
	"time"
)

type customResponse2 struct{}

func NewcustomResponse2() *customResponse2 {
	return &customResponse2{}
}
func (r *customResponse2) response(c *gin.Context, code int, result, returnInfo interface{}) {
	c.JSON(code, gin.H{
		"result":     result,
		"returnInfo": returnInfo,
	})
	return
}

type dataInfo struct {
	Result     Result       `json:"result"`
	ReturnInfo []ReturnInfo `json:"returnInfo"`
}

type Result struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	TransactionId   string `json:"transactionId"`
	TransactionTime string `json:"transactionTime"`
}
type ReturnInfo struct {
	OrderId    string `json:"orderId"`
	Msisdn     string `json:"msisdn"`
	SkuId      string `json:"skuId"`
	SkuName    string `json:"skuName"`
	OpenStatus int    `json:"openStatus"`
	OpenDesc   string `json:"openDesc"`
	OpenTime   string `json:"openTime"`
}

func (r *customResponse2) Success(c *gin.Context, data interface{}) {
	var result = dataInfo{}
	err := copier.Copy(&result, data)
	if err != nil {
		logger.Warn("copier.Copy error", zap.Error(err), zap.String("request_id", c.GetString("request_id")))
	}
	if len(result.ReturnInfo) > 0 {
		r.response(c, http.StatusOK, result.Result, result.ReturnInfo)
		return
	}
	r.response(c, http.StatusOK, result.Result, nil)
	return
}
func (r *customResponse2) Success2(c *gin.Context, code, msg string, data interface{}) {
}

func (r *customResponse2) ParamError(c *gin.Context, err error) {
	r.response(c, http.StatusOK, nil, nil)
}

func (r *customResponse2) Error(c *gin.Context, err error) bool {
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
