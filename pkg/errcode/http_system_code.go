package errcode

// http系统级别错误码，错误码范围 10000~20000
var (
	// Success 成功
	// 错误码: 0
	// 描述: 操作成功
	Success = NewError(0, "ok")

	// InvalidParams 无效参数
	// 错误码: 100001
	// 描述: 请求参数无效
	InvalidParams = NewError(100001, "Invalid Parameter")

	// Unauthorized 未授权
	// 错误码: 100002
	// 描述: 用户未被授权访问资源
	Unauthorized = NewError(100002, "Unauthorized")

	// InternalServerError 内部服务器错误
	// 错误码: 100003
	// 描述: 服务器内部发生错误
	InternalServerError = NewError(100003, "Internal Server Error")

	// NotFound 未找到
	// 错误码: 100004
	// 描述: 请求的资源未找到
	NotFound = NewError(100004, "Not Found")

	// Timeout 请求超时
	// 错误码: 100006
	// 描述: 请求超时
	Timeout = NewError(100006, "Request Timeout")

	// TooManyRequests 请求过多
	// 错误码: 100007
	// 描述: 过多的请求
	TooManyRequests = NewError(100007, "Too Many Requests")

	// Forbidden 禁止访问
	// 错误码: 100008
	// 描述: 禁止访问资源
	Forbidden = NewError(100008, "Forbidden")

	// LimitExceed 超过限制
	// 错误码: 100009
	// 描述: 超过了某个限制
	LimitExceed = NewError(100009, "Limit Exceed")

	// DeadlineExceeded 截止时间已过
	// 错误码: 100010
	// 描述: 操作超过了截止时间
	DeadlineExceeded = NewError(100010, "Deadline Exceeded")

	// AccessDenied 访问被拒绝
	// 错误码: 100011
	// 描述: 访问被拒绝
	AccessDenied = NewError(100011, "Access Denied")

	// MethodNotAllowed 方法不允许
	// 错误码: 100012
	// 描述: 请求的方法不被允许
	MethodNotAllowed = NewError(100012, "Method Not Allowed")

	// ServiceUnavailable 服务不可用
	// 错误码: 100013
	// 描述: 服务暂时不可用
	ServiceUnavailable = NewError(100013, "Service Unavailable")

	// Canceled 取消
	// 错误码: 100014
	// 描述: 操作被取消
	Canceled = NewError(100014, "Canceled")

	// Unknown 未知错误
	// 错误码: 100015
	// 描述: 未知错误
	Unknown = NewError(100015, "Unknown")

	// PermissionDenied 权限拒绝
	// 错误码: 100016
	// 描述: 没有足够的权限执行操作
	PermissionDenied = NewError(100016, "Permission Denied")

	// ResourceExhausted 资源耗尽
	// 错误码: 100017
	// 描述: 资源耗尽
	ResourceExhausted = NewError(100017, "Resource Exhausted")

	// FailedPrecondition 前提条件失败
	// 错误码: 100018
	// 描述: 前提条件失败
	FailedPrecondition = NewError(100018, "Failed Precondition")

	// Aborted 中断
	// 错误码: 100019
	// 描述: 操作被中断
	Aborted = NewError(100019, "Aborted")

	// OutOfRange 超出范围
	// 错误码: 100020
	// 描述: 请求的数据超出范围
	OutOfRange = NewError(100020, "Out Of Range")

	// Unimplemented 未实现
	// 错误码: 100021
	// 描述: 功能未实现
	Unimplemented = NewError(100021, "Unimplemented")

	// DataLoss 数据丢失
	// 错误码: 100022
	// 描述: 数据丢失
	DataLoss = NewError(100022, "Data Loss")

	// StatusBadGateway 不良网关
	// 错误码: 100023
	// 描述: 服务器作为网关或代理，从上游服务器收到无效响应
	StatusBadGateway = NewError(100023, "Bad Gateway")

	// AlreadyExists 已存在
	// 错误码: 100005
	// 描述: 资源已存在
	// 注意: 已弃用，建议使用 Conflict 替代
	AlreadyExists = NewError(100005, "Already Exists")

	// Conflict 冲突
	// 错误码: 100409
	// 描述: 请求冲突
	Conflict = NewError(100409, "Conflict")

	// TooEarly 太早
	// 错误码: 100425
	// 描述: 请求太早
	TooEarly = NewError(100425, "Too Early")
)
