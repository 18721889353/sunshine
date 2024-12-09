// Package errcode 用于定义 HTTP 和 gRPC 错误码，包括系统级别的错误码和业务级别的错误码
package errcode

import (
	"fmt"      // 用于格式化字符串
	"net/http" // 用于处理 HTTP 相关的操作
	"strconv"  // 用于字符串和数字之间的转换
	"strings"  // 用于字符串操作
)

// ToHTTPCodeLabel 需要转换为标准 HTTP 状态码的标签
const ToHTTPCodeLabel = "[standard http code]"

// errCodes 存储所有错误码及其对应的 Error 结构
var errCodes = map[int]*Error{}

// httpErrCodes 存储所有 HTTP 错误码及其对应的错误消息
var httpErrCodes = map[int]string{}

// Error 错误结构体
type Error struct {
	code    int      // 错误码
	msg     string   // 错误消息
	details []string // 错误详情

	// 如果为 true，则需要转换为标准 HTTP 状态码
	// 使用 ErrToHTTP 和 ParseError 会将此值设置为 true
	needHTTPCode bool
}

// NewError 创建一个新的错误消息
func NewError(code int, msg string, details ...string) *Error {
	if v, ok := errCodes[code]; ok {
		panic(fmt.Sprintf(`http error code = %d already exists, please define a new error code,
msg1 = %s
msg2 = %s
`, code, v.Msg(), msg))
	}

	httpErrCodes[code] = msg
	e := &Error{code: code, msg: msg, details: details}
	errCodes[code] = e
	return e
}

// Err 转换为标准错误
// 如果有参数 'msg'，则会替换原始消息
func (e *Error) Err(msg ...string) error {
	message := e.msg
	if len(msg) > 0 {
		message = strings.Join(msg, ", ")
	}

	if len(e.details) == 0 {
		return fmt.Errorf("code = %d, msg = %s", e.code, message)
	}
	return fmt.Errorf("code = %d, msg = %s, details = %v", e.code, message, e.details)
}

// ErrToHTTP 转换为标准错误并添加 ToHTTPCodeLabel 到错误消息
// 如果需要转换为标准 HTTP 状态码，可以使用此方法
// 提示：可以调用 GetErrorCode 函数获取标准 HTTP 状态码
func (e *Error) ErrToHTTP(msg ...string) error {
	message := e.msg
	if len(msg) > 0 {
		message = strings.Join(msg, ", ")
	}

	if len(e.details) == 0 {
		return fmt.Errorf("code = %d, msg = %s%s", e.code, message, ToHTTPCodeLabel)
	}
	return fmt.Errorf("code = %d, msg = %s, details = %v%s", e.code, message, strings.Join(e.details, ", "), ToHTTPCodeLabel)
}

// Code 获取错误码
func (e *Error) Code() int {
	return e.code
}

// Msg 获取错误消息
func (e *Error) Msg() string {
	return e.msg
}

// NeedHTTPCode 是否需要转换为标准 HTTP 状态码
func (e *Error) NeedHTTPCode() bool {
	return e.needHTTPCode
}

// Details 获取错误详情
func (e *Error) Details() []string {
	return e.details
}

// WithDetails 添加错误详情
func (e *Error) WithDetails(details ...string) *Error {
	newError := &Error{code: e.code, msg: e.msg}
	newError.msg += ", " + strings.Join(details, ", ")
	return newError
}

// RewriteMsg 重写错误消息
func (e *Error) RewriteMsg(msg string) *Error {
	return &Error{code: e.code, msg: msg}
}

// WithOutMsg 输出错误消息
// 已弃用：请使用 RewriteMsg 替代
func (e *Error) WithOutMsg(msg string) *Error {
	return &Error{code: e.code, msg: msg}
}

// WithOutMsgI18n 输出国际化错误消息
// langMsg 示例：
//
//	map[int]map[string]string{
//			20010: {
//				"en-US": "login failed",
//				"zh-CN": "登录失败",
//			},
//		}
//
// lang BCP 47 代码 https://learn.microsoft.com/en-us/openspecs/office_standards/ms-oe376/6c085406-a698-4e12-9d4d-c3b0ee3dbc4a
func (e *Error) WithOutMsgI18n(langMsg map[int]map[string]string, lang string) *Error {
	if i18nMsg, ok := langMsg[e.Code()]; ok {
		if msg, ok2 := i18nMsg[lang]; ok2 {
			return &Error{code: e.code, msg: msg}
		}
	}

	return &Error{code: e.code, msg: e.msg}
}

// ToHTTPCode 转换为 HTTP 状态码
func (e *Error) ToHTTPCode() int {
	switch e.Code() {
	case Success.Code():
		return http.StatusOK
	case InternalServerError.Code():
		return http.StatusInternalServerError
	case InvalidParams.Code():
		return http.StatusBadRequest
	}

	switch e.Code() {
	case Unauthorized.Code(), PermissionDenied.Code():
		return http.StatusUnauthorized
	case TooManyRequests.Code(), LimitExceed.Code():
		return http.StatusTooManyRequests
	case Forbidden.Code(), AccessDenied.Code():
		return http.StatusForbidden
	case NotFound.Code():
		return http.StatusNotFound
	case Conflict.Code(), AlreadyExists.Code():
		return http.StatusConflict
	case TooEarly.Code():
		return http.StatusTooEarly
	case Timeout.Code(), DeadlineExceeded.Code():
		return http.StatusRequestTimeout
	case MethodNotAllowed.Code():
		return http.StatusMethodNotAllowed
	case ServiceUnavailable.Code():
		return http.StatusServiceUnavailable
	case Unimplemented.Code():
		return http.StatusNotImplemented
	case StatusBadGateway.Code():
		return http.StatusBadGateway
	}

	return http.StatusInternalServerError
}

// ParseError 从错误消息中解析出错误码
func ParseError(err error) *Error {
	if err == nil {
		return Success
	}

	outError := &Error{
		code: -1,
		msg:  "unknown error",
	}

	splits := strings.Split(err.Error(), ", msg = ")
	codeStr := strings.ReplaceAll(splits[0], "code = ", "")
	code, er := strconv.Atoi(codeStr)
	if er != nil {
		return outError
	}

	if e, ok := errCodes[code]; ok {
		if len(splits) > 1 {
			outError.code = code
			outError.msg = splits[1]
			outError.needHTTPCode = strings.Contains(err.Error(), ToHTTPCodeLabel)
			return outError
		}
		return e
	}

	return outError
}

// GetErrorCode 从 HTTP 调用返回的错误中获取错误码
func GetErrorCode(err error) int {
	e := ParseError(err)
	if e.needHTTPCode {
		return e.ToHTTPCode()
	}
	return e.Code()
}

// ListHTTPErrCodes 列出 HTTP 错误码
func ListHTTPErrCodes() []ErrInfo {
	return getErrorInfo(httpErrCodes)
}
