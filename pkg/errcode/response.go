package errcode

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SkipResponse 跳过响应
var SkipResponse = errors.New("skip response") //nolint

// Responser 响应接口
type Responser interface {
	Success(ctx *gin.Context, data interface{})                        // 成功响应
	Success2(ctx *gin.Context, code int, msg string, data interface{}) // 成功响应
	ParamError(ctx *gin.Context, err error)                            // 参数错误响应
	Error(ctx *gin.Context, err error) bool                            // 错误响应，返回 true 表示已将错误代码转换为标准 HTTP 代码
}

// NewResponser 创建一个新的 Responser，如果 isFromRPC=true，表示从 RPC 返回，否则默认从 HTTP 返回
func NewResponser(isMessage, isFromRPC bool, httpErrors []*Error, rpcStatus []*RPCStatus) Responser {
	httpErrorsMap := make(map[int]*Error)
	rpcStatusMap := make(map[int]*RPCStatus)

	for _, httpError := range httpErrors {
		if httpError == nil {
			continue
		}
		httpErrorsMap[httpError.Code()] = httpError
	}
	for _, statusError := range rpcStatus {
		if statusError == nil || statusError.status == nil {
			continue
		}
		rpcStatusMap[int(statusError.ToRPCCode())] = statusError
		rpcStatusMap[int(statusError.status.Code())] = statusError
	}

	return &defaultResponse{
		isFromRPC:  isFromRPC,
		httpErrors: httpErrorsMap,
		rpcStatus:  rpcStatusMap,
		isMessage:  isMessage,
	}
}

// defaultResponse 默认响应实现
type defaultResponse struct {
	isFromRPC  bool               // 错误是否来自 gRPC，如果不是，默认来自 HTTP
	httpErrors map[int]*Error     // HTTP 错误映射
	rpcStatus  map[int]*RPCStatus // gRPC 状态映射
	isMessage  bool               //返回消息是 message
}

// response 构建 JSON 响应
func (resp *defaultResponse) response(c *gin.Context, respStatus, code int, msg string, data interface{}) {
	if resp.isMessage {
		c.JSON(respStatus, map[string]interface{}{
			"code":    code,
			"message": msg,
			"data":    data,
		})
	} else {
		c.JSON(respStatus, map[string]interface{}{
			"code": code,
			"msg":  msg,
			"data": data,
		})
	}
}

// Success 成功响应
func (resp *defaultResponse) Success(c *gin.Context, data interface{}) {
	resp.response(c, http.StatusOK, 0, "ok", data)
}
func (resp *defaultResponse) Success2(c *gin.Context, code int, msg string, data interface{}) {
	resp.response(c, http.StatusOK, code, msg, data)
}

// ParamError 参数错误响应
func (resp *defaultResponse) ParamError(c *gin.Context, _ error) {
	resp.response(c, http.StatusOK, InvalidParams.Code(), InvalidParams.Msg(), struct{}{})
}

// Error 错误响应
func (resp *defaultResponse) Error(c *gin.Context, err error) bool {
	if resp.isFromRPC {
		// 错误来自 gRPC 并响应相应的 HTTP 代码
		return resp.handleRPCError(c, err)
	}

	// 错误来自 HTTP 并响应 HTTP 代码
	return resp.handleHTTPError(c, err)
}

// handleRPCError 处理来自 gRPC 的错误
func (resp *defaultResponse) handleRPCError(c *gin.Context, err error) bool {
	st, _ := status.FromError(err)

	// 用户自定义错误，响应 200
	if st.Code() == codes.Unknown {
		code, msg := parseCodeAndMsg(st.String())
		if code == -1 {
			// 不符合规范的错误
			resp.response(c, http.StatusOK, -1, "unknown error", struct{}{})
		} else {
			// 使用 NewRPCStatus 创建的错误
			resp.response(c, http.StatusOK, code, msg, struct{}{})
		}
		return false
	}

	// 默认错误代码转换为 HTTP
	switch st.Code() {
	case codes.Internal, StatusInternalServerError.status.Code():
		resp.response(c, http.StatusInternalServerError, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError), struct{}{})
		return true
	case codes.Unavailable, StatusServiceUnavailable.status.Code():
		resp.response(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable), struct{}{})
		return true
	}

	// 检查是否需要返回标准 HTTP 代码
	if strings.Contains(st.Message(), ToHTTPCodeLabel) {
		code := convertToHTTPCode(st.Code())
		msg := strings.ReplaceAll(st.Message(), ToHTTPCodeLabel, "")
		resp.response(c, code, int(st.Code()), msg, struct{}{})
		return true
	}

	// 用户自定义错误代码转换为 HTTP
	if resp.isUserDefinedRPCErrorCode(c, int(st.Code())) {
		return true
	}

	// 响应 200
	resp.response(c, http.StatusOK, int(st.Code()), st.Message(), struct{}{})

	return false
}

// handleHTTPError 处理来自 HTTP 的错误
func (resp *defaultResponse) handleHTTPError(c *gin.Context, err error) bool {
	e := ParseError(err)

	// 默认错误代码转换为 HTTP
	switch e.Code() {
	case InternalServerError.Code(), http.StatusInternalServerError:
		resp.response(c, http.StatusInternalServerError, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError), struct{}{})
		return true
	case ServiceUnavailable.Code(), http.StatusServiceUnavailable:
		resp.response(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable), struct{}{})
		return true
	}

	// 用户请求返回标准 HTTP 代码，如果 e.ToHTTPCode() 不匹配，则返回 500
	if e.needHTTPCode {
		msg := strings.ReplaceAll(e.msg, ToHTTPCodeLabel, "")
		resp.response(c, e.ToHTTPCode(), e.code, msg, struct{}{})
		return true
	}

	// 用户自定义错误代码转换为 HTTP
	if resp.isUserDefinedHTTPErrorCode(c, e.Code()) {
		return true
	}

	// 响应 200
	resp.response(c, http.StatusOK, e.code, e.msg, struct{}{})
	return false
}

// isUserDefinedRPCErrorCode 检查是否为用户自定义的 gRPC 错误代码
func (resp *defaultResponse) isUserDefinedRPCErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.rpcStatus[errCode]; ok {
		httpCode := ToHTTPErr(v.status).ToHTTPCode()
		msg := http.StatusText(httpCode)
		if msg == "" {
			msg = "unknown error"
		}
		resp.response(c, httpCode, httpCode, msg, struct{}{})
		return true
	}
	return false
}

// isUserDefinedHTTPErrorCode 检查是否为用户自定义的 HTTP 错误代码
func (resp *defaultResponse) isUserDefinedHTTPErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.httpErrors[errCode]; ok {
		httpCode := v.ToHTTPCode()
		msg := http.StatusText(httpCode)
		if msg == "" {
			msg = "unknown error"
		}
		resp.response(c, httpCode, httpCode, msg, struct{}{})
		return true
	}
	return false
}

// ToHTTPErr 将 gRPC 状态转换为 HTTP 错误
func ToHTTPErr(st *status.Status) *Error { //nolint
	switch st.Code() {
	case StatusSuccess.status.Code(), codes.OK:
		return Success
	case StatusInvalidParams.status.Code(), codes.InvalidArgument:
		return InvalidParams
	case StatusInternalServerError.status.Code(), codes.Internal:
		return InternalServerError
	case StatusUnimplemented.status.Code(), codes.Unimplemented:
		return Unimplemented
	case StatusPermissionDenied.status.Code(), codes.PermissionDenied:
		return PermissionDenied
	}

	switch st.Code() {
	case StatusCanceled.status.Code(), codes.Canceled:
		return Canceled
	case StatusUnknown.status.Code(), codes.Unknown:
		return Unknown
	case StatusDeadlineExceeded.status.Code(), codes.DeadlineExceeded:
		return DeadlineExceeded
	case StatusNotFound.status.Code(), codes.NotFound:
		return NotFound
	case StatusAlreadyExists.status.Code(), codes.AlreadyExists, StatusConflict.status.Code():
		return Conflict
	case StatusResourceExhausted.status.Code(), codes.ResourceExhausted:
		return ResourceExhausted
	case StatusFailedPrecondition.status.Code(), codes.FailedPrecondition:
		return FailedPrecondition
	case StatusAborted.status.Code(), codes.Aborted:
		return Aborted
	case StatusOutOfRange.status.Code(), codes.OutOfRange:
		return OutOfRange
	case StatusServiceUnavailable.status.Code(), codes.Unavailable:
		return ServiceUnavailable
	case StatusDataLoss.status.Code(), codes.DataLoss:
		return DataLoss
	case StatusUnauthorized.status.Code(), codes.Unauthenticated:
		return Unauthorized

	case StatusAccessDenied.status.Code():
		return AccessDenied
	case StatusLimitExceed.status.Code():
		return LimitExceed
	case StatusMethodNotAllowed.status.Code():
		return MethodNotAllowed
	}

	return &Error{
		code: int(st.Code()),
		msg:  st.Message(),
	}
}

// parseCodeAndMsg 解析错误字符串中的代码和消息
func parseCodeAndMsg(errStr string) (int, string) {
	if errStr != "" {
		ss := strings.Split(errStr, "desc = ")
		cm := strings.Split(ss[len(ss)-1], "msg = ")
		if len(cm) == 2 {
			codeStr := strings.ReplaceAll(cm[0], "code = ", "")
			codeStr = strings.ReplaceAll(codeStr, ", ", "")
			code, _ := strconv.Atoi(codeStr)
			msg := cm[1]
			return code, msg
		}
	}
	return -1, errStr
}
