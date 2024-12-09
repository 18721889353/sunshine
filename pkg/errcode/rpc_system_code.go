package errcode

// rpc system level error code with status prefix, error code range 30000~40000
// RPC系统级别错误代码，错误代码范围为30000到40000
var (
	StatusSuccess = NewRPCStatus(0, "ok") // 操作成功

	StatusCanceled            = NewRPCStatus(300001, "Canceled")              // 操作已取消
	StatusUnknown             = NewRPCStatus(300002, "Unknown")               // 未知错误
	StatusInvalidParams       = NewRPCStatus(300003, "Invalid Parameter")     // 参数无效
	StatusDeadlineExceeded    = NewRPCStatus(300004, "Deadline Exceeded")     // 超时
	StatusNotFound            = NewRPCStatus(300005, "Not Found")             // 未找到
	StatusAlreadyExists       = NewRPCStatus(300006, "Already Exists")        // 已存在
	StatusPermissionDenied    = NewRPCStatus(300007, "Permission Denied")     // 权限被拒绝
	StatusResourceExhausted   = NewRPCStatus(300008, "Resource Exhausted")    // 资源耗尽
	StatusFailedPrecondition  = NewRPCStatus(300009, "Failed Precondition")   // 预先条件失败
	StatusAborted             = NewRPCStatus(300010, "Aborted")               // 操作被中止
	StatusOutOfRange          = NewRPCStatus(300011, "Out Of Range")          // 范围外
	StatusUnimplemented       = NewRPCStatus(300012, "Unimplemented")         // 未实现
	StatusInternalServerError = NewRPCStatus(300013, "Internal Server Error") // 内部服务器错误
	StatusServiceUnavailable  = NewRPCStatus(300014, "Service Unavailable")   // 服务不可用
	StatusDataLoss            = NewRPCStatus(300015, "Data Loss")             // 数据丢失
	StatusUnauthorized        = NewRPCStatus(300016, "Unauthorized")          // 未授权

	StatusTimeout          = NewRPCStatus(300017, "Request Timeout")    // 请求超时
	StatusTooManyRequests  = NewRPCStatus(300018, "Too Many Requests")  // 请求过多
	StatusForbidden        = NewRPCStatus(300019, "Forbidden")          // 禁止访问
	StatusLimitExceed      = NewRPCStatus(300020, "Limit Exceed")       // 超出限制
	StatusMethodNotAllowed = NewRPCStatus(300021, "Method Not Allowed") // 方法不允许
	StatusAccessDenied     = NewRPCStatus(300022, "Access Denied")      // 访问被拒绝
	StatusConflict         = NewRPCStatus(300023, "Conflict")           // 冲突
)
