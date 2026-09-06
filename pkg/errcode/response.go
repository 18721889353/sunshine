// Package errcode 提供统一的 HTTP/GRPC 错误码及响应封装。
// 核心功能：
//   - 定义业务错误码与标准 HTTP 状态码的映射关系
//   - 提供统一的 JSON 响应格式（code, msg, data）
//   - 支持结构体拆包（unwrap），使单切片响应可直接返回数组，简化前端处理
//   - 兼容 gRPC 状态码与 HTTP 错误码的转换
package errcode

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// SkipResponse 用于中间件或业务逻辑中跳过后续响应处理（如已自行处理）
	//nolint:revive // 保留命名以兼容已有代码
	SkipResponse = errors.New("skip response")
	// unwrapCache 缓存结构体拆包分析结果，避免重复反射造成性能损失
	unwrapCache sync.Map // map[reflect.Type]unwrapInfo
)

// unwrapInfo 存储结构体是否需要拆包以及目标字段索引
type unwrapInfo struct {
	shouldUnwrap bool // 是否需要拆包（仅当结构体只有一个导出字段且为切片时）
	fieldIndex   int  // 若拆包，该字段的索引位置
}

// Responser 定义统一响应接口，所有响应实现必须遵循此接口
type Responser interface {
	// Success 返回成功响应，code=0, msg="ok"，data 自动拆包
	Success(ctx *gin.Context, data any)
	// Success2 自定义成功响应，允许指定 code 和 msg
	Success2(ctx *gin.Context, code, msg string, data any)
	// ParamError 参数错误响应（通常由 validation 校验失败触发）
	ParamError(ctx *gin.Context, err error)
	// Error 错误响应，根据错误类型自动切换 HTTP/RPC 处理，返回布尔值表示是否已处理
	Error(ctx *gin.Context, err error) bool
}

// defaultResponse 默认实现，支持通过选项配置响应行为
type defaultResponse struct {
	isFromRPC  bool               // 错误是否来自 gRPC（影响错误处理分支）
	httpErrors map[int]*Error     // 自定义 HTTP 错误映射（业务码 → HTTP 状态码）
	rpcStatus  map[int]*RPCStatus // 自定义 RPC 状态映射
	isMessage  bool               // 响应中使用 "message" 还是 "msg" 字段
}

// NewResponser 创建新的响应器实例，传入配置选项
func NewResponser(isMessage, isFromRPC bool, httpErrors []*Error, rpcStatus []*RPCStatus) Responser {
	// 构建 HTTP 错误映射表（业务码 → *Error）
	var httpErrorsMap map[int]*Error
	if len(httpErrors) > 0 {
		httpErrorsMap = make(map[int]*Error, len(httpErrors))
		for _, httpError := range httpErrors {
			if httpError != nil {
				httpErrorsMap[httpError.Code()] = httpError
			}
		}
	}

	// 构建 RPC 状态映射表（gRPC 状态码 → *RPCStatus）
	var rpcStatusMap map[int]*RPCStatus
	if len(rpcStatus) > 0 {
		rpcStatusMap = make(map[int]*RPCStatus, len(rpcStatus)*2)
		for _, statusError := range rpcStatus {
			if statusError == nil || statusError.status == nil {
				continue
			}
			// 同时存储两种可能的 key：业务码和标准 gRPC 码
			rpcStatusMap[int(statusError.ToRPCCode())] = statusError
			rpcStatusMap[int(statusError.status.Code())] = statusError
		}
	}

	return &defaultResponse{
		isFromRPC:  isFromRPC,
		httpErrors: httpErrorsMap,
		rpcStatus:  rpcStatusMap,
		isMessage:  isMessage,
	}
}

// response 核心响应构建方法，所有响应最终汇聚至此
// 功能：
//   - 将 data=nil 或空切片转为空对象 {}，保证数据结构一致性
//   - 封装 code/msg/data 至顶层 JSON
//   - 根据 isMessage 决定使用 "message" 或 "msg" 字段
func (resp *defaultResponse) response(c *gin.Context, respStatus int, code, msg string, data any) {
	// 规范处理：错误响应或未设置数据时，data 应为空对象 {} 而非 null 或 []
	if data == nil {
		data = make(map[string]interface{}, 0)
	} else {
		v := reflect.ValueOf(data)
		if v.Kind() == reflect.Slice && v.Len() == 0 {
			data = make(map[string]interface{}, 0)
		}
	}

	// 构建最终返回的 JSON 对象
	respData := make(map[string]any, 3)
	respData["code"] = code
	respData["data"] = data

	if resp.isMessage {
		respData["message"] = msg
	} else {
		respData["msg"] = msg
	}

	c.JSON(respStatus, respData)
}

// Success 成功响应，自动拆包 data，HTTP 状态码 200
func (resp *defaultResponse) Success(c *gin.Context, data any) {
	finalData := resp.unwrapData(data)
	resp.response(c, http.StatusOK, "0", "ok", finalData)
}

// Success2 自定义成功响应，允许指定 code 和 msg，不自动拆包
func (resp *defaultResponse) Success2(c *gin.Context, code, msg string, data any) {
	resp.response(c, http.StatusOK, code, msg, data)
}

// respondWithHTTPCode 根据 HTTP 状态码生成标准响应（主要用于非 200 错误）
func (resp *defaultResponse) respondWithHTTPCode(c *gin.Context, httpCode int) {
	resp.response(c, httpCode, cast.ToString(httpCode), getStatusTextOrDefault(httpCode), nil)
}

// ParamError 参数错误响应，固定使用 errcode.InvalidParams 的错误码和消息
func (resp *defaultResponse) ParamError(c *gin.Context, _ error) {
	resp.response(c, http.StatusOK, cast.ToString(InvalidParams.Code()), InvalidParams.Msg(), nil)
}

// Error 错误响应入口，根据配置选择 RPC 或 HTTP 处理分支
func (resp *defaultResponse) Error(c *gin.Context, err error) bool {
	if resp.isFromRPC {
		return resp.handleRPCError(c, err)
	}
	return resp.handleHTTPError(c, err)
}

// unwrapData 核心拆包逻辑：对结构体进行分析，如果结构体只包含一个导出字段且为切片，则直接返回该切片
// 目的：方便前端直接获取列表数据，无需额外取值（如 data.list → data）
// 缓存结果以提高性能
func (resp *defaultResponse) unwrapData(data any) any {
	if data == nil {
		return nil
	}

	v := reflect.ValueOf(data)
	// 解引用指针，直到非指针类型
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	// 非结构体直接返回
	if v.Kind() != reflect.Struct {
		return data
	}

	typ := v.Type()
	// 从缓存获取分析结果
	if cached, ok := unwrapCache.Load(typ); ok {
		info, ok := cached.(unwrapInfo)
		if !ok {
			return data
		}
		if info.shouldUnwrap {
			return v.Field(info.fieldIndex).Interface()
		}
		return data
	}

	// 缓存未命中，执行分析并缓存
	return resp.analyzeAndUnwrap(v, typ)
}

// analyzeAndUnwrap 分析结构体字段，决定是否需要拆包
// 规则：只有一个导出（首字母大写）且非匿名字段，且该字段类型为切片，才进行拆包
// 这样可以兼容多字段结构体（如分页响应），避免误拆包
func (resp *defaultResponse) analyzeAndUnwrap(v reflect.Value, typ reflect.Type) any {
	var firstSliceIdx = -1
	var sliceCount int
	var exportedCount int // 统计导出字段总数

	numField := typ.NumField()
	for i := 0; i < numField; i++ {
		field := typ.Field(i)
		// 导出且非匿名字段
		if field.PkgPath == "" && !field.Anonymous {
			exportedCount++
			if field.Type.Kind() == reflect.Slice {
				sliceCount++
				if sliceCount == 1 {
					firstSliceIdx = i
				}
			}
		}
	}

	// 只有唯一一个导出字段且为切片时才拆包
	shouldUnwrap := (exportedCount == 1 && sliceCount == 1)

	info := unwrapInfo{
		shouldUnwrap: shouldUnwrap,
		fieldIndex:   firstSliceIdx,
	}
	unwrapCache.Store(typ, info)

	if shouldUnwrap {
		return v.Field(firstSliceIdx).Interface()
	}
	return v.Interface()
}

// handleRPCError 处理来自 gRPC 的错误，根据状态码映射到适当的 HTTP 响应
func (resp *defaultResponse) handleRPCError(c *gin.Context, err error) bool {
	st, _ := status.FromError(err)
	stCode := st.Code()

	// codes.Unknown 通常由业务自定义错误码，尝试解析 code 和 msg
	if stCode == codes.Unknown {
		code, msg := parseCodeAndMsg(st.String())
		if code == -1 {
			resp.response(c, http.StatusOK, "-1", "unknown error", nil)
		} else {
			resp.response(c, http.StatusOK, cast.ToString(code), msg, nil)
		}
		return false
	}

	// 标准 gRPC 错误码映射到对应的 HTTP 状态码
	switch stCode {
	case codes.Internal, StatusInternalServerError.status.Code():
		resp.respondWithHTTPCode(c, http.StatusInternalServerError)
		return true
	case codes.Unavailable, StatusServiceUnavailable.status.Code():
		resp.respondWithHTTPCode(c, http.StatusServiceUnavailable)
		return true
	}

	// 若错误消息包含指定标签，强制转换为指定 HTTP 状态码
	if strings.Contains(st.Message(), ToHTTPCodeLabel) {
		httpCode := convertToHTTPCode(stCode)
		msg := strings.ReplaceAll(st.Message(), ToHTTPCodeLabel, "")
		resp.response(c, httpCode, cast.ToString(int(stCode)), msg, nil)
		return true
	}

	// 检查用户自定义的 RPC 映射
	if resp.rpcStatus != nil && resp.isUserDefinedRPCErrorCode(c, int(stCode)) {
		return true
	}

	// 默认：返回 200 OK，body 中包含业务错误码
	resp.response(c, http.StatusOK, cast.ToString(int(stCode)), st.Message(), nil)
	return false
}

// handleHTTPError 处理来自 HTTP 的错误（非 gRPC）
func (resp *defaultResponse) handleHTTPError(c *gin.Context, err error) bool {
	e := ParseError(err)

	// 标准 HTTP 错误码映射
	switch e.Code() {
	case InternalServerError.Code(), http.StatusInternalServerError:
		resp.respondWithHTTPCode(c, http.StatusInternalServerError)
		return true
	case ServiceUnavailable.Code(), http.StatusServiceUnavailable:
		resp.respondWithHTTPCode(c, http.StatusServiceUnavailable)
		return true
	}

	// 若用户设置需要返回标准 HTTP 状态码
	if e.needHTTPCode {
		msg := strings.ReplaceAll(e.msg, ToHTTPCodeLabel, "")
		resp.response(c, e.ToHTTPCode(), cast.ToString(e.code), msg, nil)
		return true
	}

	// 检查用户自定义的 HTTP 错误映射
	if resp.httpErrors != nil && resp.isUserDefinedHTTPErrorCode(c, e.Code()) {
		return true
	}

	// 默认：返回 200 OK，body 中包含业务错误码
	resp.response(c, http.StatusOK, cast.ToString(e.code), e.msg, nil)
	return false
}

// isUserDefinedRPCErrorCode 检查是否在自定义 RPC 映射中，若存在则使用对应的 HTTP 状态码响应
func (resp *defaultResponse) isUserDefinedRPCErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.rpcStatus[errCode]; ok {
		resp.respondWithHTTPCode(c, ToHTTPErr(v.status).ToHTTPCode())
		return true
	}
	return false
}

// isUserDefinedHTTPErrorCode 检查是否在自定义 HTTP 错误映射中，若存在则使用对应的 HTTP 状态码响应
func (resp *defaultResponse) isUserDefinedHTTPErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.httpErrors[errCode]; ok {
		resp.respondWithHTTPCode(c, v.ToHTTPCode())
		return true
	}
	return false
}

// getStatusTextOrDefault 获取 HTTP 状态文本，若不存在则返回 "unknown error"
func getStatusTextOrDefault(code int) string {
	msg := http.StatusText(code)
	if msg == "" {
		return "unknown error"
	}
	return msg
}

// ToHTTPErr 将 gRPC 状态转换为对应的 *Error，适用于业务错误码与 gRPC 码的桥接
func ToHTTPErr(st *status.Status) *Error {
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
	case StatusServiceUnavailable.status.Code(), codes.Unavailable:
		return ServiceUnavailable
	case StatusNotFound.status.Code(), codes.NotFound:
		return NotFound
	case StatusAlreadyExists.status.Code(), codes.AlreadyExists, StatusConflict.status.Code():
		return Conflict
	case StatusUnauthorized.status.Code(), codes.Unauthenticated:
		return Unauthorized
	case StatusCanceled.status.Code(), codes.Canceled:
		return Canceled
	case StatusUnknown.status.Code(), codes.Unknown:
		return Unknown
	case StatusDeadlineExceeded.status.Code(), codes.DeadlineExceeded:
		return DeadlineExceeded
	case StatusResourceExhausted.status.Code(), codes.ResourceExhausted:
		return ResourceExhausted
	case StatusFailedPrecondition.status.Code(), codes.FailedPrecondition:
		return FailedPrecondition
	case StatusAborted.status.Code(), codes.Aborted:
		return Aborted
	case StatusOutOfRange.status.Code(), codes.OutOfRange:
		return OutOfRange
	case StatusDataLoss.status.Code(), codes.DataLoss:
		return DataLoss
	case StatusAccessDenied.status.Code():
		return AccessDenied
	case StatusLimitExceed.status.Code():
		return LimitExceed
	case StatusMethodNotAllowed.status.Code():
		return MethodNotAllowed
	}

	// 未匹配时回退为通用错误
	return &Error{
		code: int(st.Code()),
		msg:  st.Message(),
	}
}

// parseCodeAndMsg 从 gRPC 错误字符串中提取业务错误码和消息
// 解析格式示例：rpc error: code = Code(403403) desc = code = 403403, msg = token已过期
func parseCodeAndMsg(errStr string) (int, string) {
	if errStr == "" {
		return -1, ""
	}

	descIdx := strings.LastIndex(errStr, "desc = ")
	if descIdx == -1 {
		return -1, errStr
	}

	lastPart := errStr[descIdx+7:] // 跳过 "desc = "
	cm := strings.Split(lastPart, "msg = ")
	if len(cm) == 2 {
		codeStr := cm[0]
		codeStr = strings.TrimPrefix(codeStr, "code = ")
		codeStr = strings.TrimSuffix(codeStr, ", ")
		code := cast.ToInt(strings.TrimSpace(codeStr))
		return code, cm[1]
	}

	return -1, errStr
}
