// Package tkerrors provides error codes and types for the TK SDK.
package tkerrors

import "fmt"

// ErroCode 错误代码类型
type ErroCode int

const (
	// ConfigIsNull 配置为空错误
	ConfigIsNull ErroCode = 10001
	// ParamError 参数错误
	ParamError            ErroCode = 10002
	HTTPError             ErroCode = 10003 // nolint: revive // backward compatibility
	GetTokenError         ErroCode = 10004
	ConfigAppIDIsNull     ErroCode = 10005 // nolint: revive // backward compatibility
	ConfigAppSecretIsNull ErroCode = 10006
)

var errMessageMap = map[ErroCode]string{
	ConfigIsNull:          "Config配置为空",
	ParamError:            "参数错误",
	HTTPError:             "处理Http请求错误",
	GetTokenError:         "获取Token错误",
	ConfigAppIDIsNull:     "Config中AppId为空",
	ConfigAppSecretIsNull: "Config中AppSecret为空",
}

// TkError 途刻SDK错误结构
type TkError struct {
	Code    ErroCode
	Message string
}

func (err *TkError) Error() string {
	return fmt.Sprintf("Code: %d, Message: %s", err.Code, err.Message)
}

// NewTkErrorWithMessage 创建带消息的途刻错误
func NewTkErrorWithMessage(code ErroCode, message string) error {
	return &TkError{
		Code:    code,
		Message: message,
	}
}

// NewTkError 创建途刻错误
func NewTkError(code ErroCode) error {
	return &TkError{
		Code:    code,
		Message: errMessageMap[code],
	}
}
