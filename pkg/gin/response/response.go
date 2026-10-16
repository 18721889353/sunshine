// Package response 是 gin HTTP 响应的适配层，统一收口到 pkg/errcode 全局 ResponseFormat：
// 响应体字段名、code/msg 内容与序列化类型均按配置文件 response 段实时生效，
// 与 gRPC 网关(http-pb)链路输出格式一致，不再维护独立的写死格式。
//
// 核心功能：
//   - Success：成功响应，按配置输出 SuccessCode/SuccessMsg（未配置默认数字 0/"ok"），
//     支持业务码提取（extractCustomCode）与单切片结构体拆包。
//   - Error：平铺 200 响应，body code 为 errcode.Error 业务码（数字），data 透传。
//   - Output/Out：标准 HTTP 状态码响应，body code 为 HTTP 状态码（数字）。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/errcode"
)

// responser 复用 errcode 默认响应器：每次响应实时读取全局 ResponseFormat，天然支持配置热更新
var responser = errcode.NewResponser(false, false, nil, nil)

// firstData 统一取第一个数据项（业务调用点最多传一个数据），未传时为 nil，由配置决定序列化为 {} 或 []
func firstData(data []interface{}) interface{} {
	if len(data) > 0 {
		return data[0]
	}
	return nil
}

// Success 成功响应：code/msg/字段名按配置文件 response 段输出（默认成功码为数字 0、提示 "ok"，
// 配成带引号的字符串则输出字符串），data 的 nil/空值按 nilDataAsEmptyArray 序列化为 [] 或 {}；
// extractCustomCode=true 时从 data 中提取 CustomCode 字段（或 map 的 code 键）作为业务码并剥离该字段
func Success(c *gin.Context, data ...interface{}) {
	responser.Success(c, firstData(data))
}

// Error 返回 200 平铺响应，body code 为业务错误码（数字），msg 为错误提示；data 透传
func Error(c *gin.Context, err *errcode.Error, data ...interface{}) {
	errcode.Respond(c, http.StatusOK, err.Code(), err.Msg(), firstData(data))
}

// Output 返回标准 HTTP 状态码响应（状态码同时作为 body code 数字输出），msg 按状态码映射 errcode 提示文案
func Output(c *gin.Context, code int, data ...interface{}) {
	errcode.Respond(c, code, code, statusMsg(code), firstData(data))
}

// statusMsg 将 HTTP 状态码映射为对应的 errcode 提示文案，未知状态码回落到标准状态文本
func statusMsg(code int) string {
	switch code {
	case http.StatusOK:
		return "ok"
	case http.StatusBadRequest:
		return errcode.InvalidParams.Msg()
	case http.StatusUnauthorized:
		return errcode.Unauthorized.Msg()
	case http.StatusForbidden:
		return errcode.Forbidden.Msg()
	case http.StatusNotFound:
		return errcode.NotFound.Msg()
	case http.StatusRequestTimeout:
		return errcode.Timeout.Msg()
	case http.StatusConflict:
		return errcode.Conflict.Msg()
	case http.StatusInternalServerError:
		return errcode.InternalServerError.Msg()
	case http.StatusTooManyRequests:
		return errcode.LimitExceed.Msg()
	case http.StatusServiceUnavailable:
		return errcode.ServiceUnavailable.Msg()
	default:
		return http.StatusText(code)
	}
}

// standardHTTPStatuses Out 认可的标准 HTTP 状态码白名单, 白名单外的状态码回落到 510 Not Extended（保持原实现语义）
var standardHTTPStatuses = map[int]struct{}{
	http.StatusOK:                  {},
	http.StatusBadRequest:          {},
	http.StatusUnauthorized:        {},
	http.StatusForbidden:           {},
	http.StatusNotFound:            {},
	http.StatusMethodNotAllowed:    {},
	http.StatusRequestTimeout:      {},
	http.StatusConflict:            {},
	http.StatusTooManyRequests:     {},
	http.StatusInternalServerError: {},
	http.StatusServiceUnavailable:  {},
}

// Out 返回标准 HTTP 状态码响应，状态码取 err.ToHTTPCode()（白名单外回落 510），msg 取错误提示（200 时为 "ok"），状态码同时作为 body code 数字输出
func Out(c *gin.Context, err *errcode.Error, data ...interface{}) {
	code := err.ToHTTPCode()
	if _, ok := standardHTTPStatuses[code]; !ok {
		code = http.StatusNotExtended
	}
	msg := err.Msg()
	if code == http.StatusOK {
		msg = "ok"
	}
	errcode.Respond(c, code, code, msg, firstData(data))
}
