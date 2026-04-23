// Package errors provides error codes and types for the TK SDK.
package errors

import "fmt"

type ErroCode int

const (
	ConfigIsNull          ErroCode = 10001
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

type TkError struct {
	Code    ErroCode
	Message string
}

func (err *TkError) Error() string {
	return fmt.Sprintf("Code: %d, Message: %s", err.Code, err.Message)
}

func NewTkErrorWithMessage(code ErroCode, message string) error {
	return &TkError{
		Code:    code,
		Message: message,
	}
}

func NewTkError(code ErroCode) error {
	return &TkError{
		Code:    code,
		Message: errMessageMap[code],
	}
}
