package errcode

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// SkipResponse 跳过响应
	SkipResponse = errors.New("skip response") //nolint
	// unwrapCache 用于缓存结构体拆包分析结果，避免重复反射
	unwrapCache sync.Map // map[reflect.Type]unwrapInfo
)

// unwrapInfo 存储结构体是否需要拆包的元数据
type unwrapInfo struct {
	shouldUnwrap bool
	fieldIndex   int // 仅当 shouldUnwrap 为 true 时有效
}

// Responser 响应接口
type Responser interface {
	Success(ctx *gin.Context, data any)                    // 成功响应
	Success2(ctx *gin.Context, code, msg string, data any) // 自定义成功响应
	ParamError(ctx *gin.Context, err error)                // 参数错误响应
	Error(ctx *gin.Context, err error) bool                // 错误响应，返回 true 表示已处理
}

// defaultResponse 默认响应实现
type defaultResponse struct {
	isFromRPC  bool               // 错误是否来自 gRPC
	httpErrors map[int]*Error     // HTTP 错误映射
	rpcStatus  map[int]*RPCStatus // gRPC 状态映射
	isMessage  bool               // 返回字段是 message 还是 msg
}

// NewResponser 创建一个新的 Responser
func NewResponser(isMessage, isFromRPC bool, httpErrors []*Error, rpcStatus []*RPCStatus) Responser {
	// 优化：预分配 map 容量，避免多次内存分配
	var httpErrorsMap map[int]*Error
	if len(httpErrors) > 0 {
		httpErrorsMap = make(map[int]*Error, len(httpErrors))
		for _, httpError := range httpErrors {
			if httpError != nil {
				httpErrorsMap[httpError.Code()] = httpError
			}
		}
	}

	var rpcStatusMap map[int]*RPCStatus
	if len(rpcStatus) > 0 {
		rpcStatusMap = make(map[int]*RPCStatus, len(rpcStatus)*2) // 一个 status 可能存两份key
		for _, statusError := range rpcStatus {
			if statusError == nil || statusError.status == nil {
				continue
			}
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

// response 统一构建 JSON 响应
func (resp *defaultResponse) response(c *gin.Context, respStatus int, code, msg string, data any) {
	respData := map[string]any{
		"code": code,
		"data": data,
	}

	if resp.isMessage {
		respData["message"] = msg
	} else {
		respData["msg"] = msg
	}

	c.JSON(respStatus, respData)
}

// Success 成功响应
func (resp *defaultResponse) Success(c *gin.Context, data any) {
	// 自动拆包逻辑
	finalData := resp.unwrapData(data)
	resp.response(c, http.StatusOK, "0", "ok", finalData)
}

// Success2 自定义 Code 和 Msg 的成功响应
func (resp *defaultResponse) Success2(c *gin.Context, code, msg string, data any) {
	resp.response(c, http.StatusOK, code, msg, data)
}

// ParamError 参数错误响应
func (resp *defaultResponse) ParamError(c *gin.Context, _ error) {
	resp.response(c, http.StatusOK, strconv.Itoa(InvalidParams.Code()), InvalidParams.Msg(), nil)
}

// Error 错误响应
func (resp *defaultResponse) Error(c *gin.Context, err error) bool {
	if resp.isFromRPC {
		return resp.handleRPCError(c, err)
	}
	return resp.handleHTTPError(c, err)
}

// unwrapData 通用的拆包函数 (优化版)
func (resp *defaultResponse) unwrapData(data any) any {
	if data == nil {
		return nil
	}

	v := reflect.ValueOf(data)

	// 1. 处理多级指针，获取到底层的 Element
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	// 2. 只针对结构体进行处理
	if v.Kind() != reflect.Struct {
		return data
	}

	typ := v.Type()

	// 3. 快速路径：尝试从缓存读取分析结果
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

	// 4. 慢速路径：执行反射分析并缓存
	return resp.analyzeAndUnwrap(v, typ)
}

// analyzeAndUnwrap 分析结构体并决定是否拆包
func (resp *defaultResponse) analyzeAndUnwrap(v reflect.Value, typ reflect.Type) any {
	var firstSliceIdx = -1
	var sliceCount int

	numField := typ.NumField()
	for i := 0; i < numField; i++ {
		field := typ.Field(i)

		// 优化点：
		// 1. PkgPath == "" 确保是导出字段 (Exported)
		// 2. !field.Anonymous 排除嵌入(组合)字段，避免逻辑歧义
		// 3. 仅查找 Slice 类型
		if field.PkgPath == "" && !field.Anonymous && field.Type.Kind() == reflect.Slice {
			sliceCount++
			if sliceCount == 1 {
				firstSliceIdx = i
				continue
			}
			// 发现超过一个切片，不符合拆包条件，直接停止
			break
		}
	}

	shouldUnwrap := (sliceCount == 1)
	info := unwrapInfo{
		shouldUnwrap: shouldUnwrap,
		fieldIndex:   firstSliceIdx,
	}

	// 写入缓存，并发场景下即使多次写入也是幂等的，无需锁
	unwrapCache.Store(typ, info)

	if shouldUnwrap {
		return v.Field(firstSliceIdx).Interface()
	}

	// 如果不需要拆包，返回经过 Elem() 处理后的原始结构体值
	// 注意：如果原数据是指针，这里会返回结构体值，保持行为一致性
	return v.Interface()
}

// handleRPCError 处理来自 gRPC 的错误
func (resp *defaultResponse) handleRPCError(c *gin.Context, err error) bool {
	st, _ := status.FromError(err)

	// 1. 处理 codes.Unknown (通常是自定义错误或 panic)
	if st.Code() == codes.Unknown {
		code, msg := parseCodeAndMsg(st.String())
		if code == -1 {
			resp.response(c, http.StatusOK, "-1", "unknown error", nil)
		} else {
			resp.response(c, http.StatusOK, strconv.Itoa(code), msg, nil)
		}
		return false
	}

	// 2. 处理标准 gRPC 错误映射到 HTTP 状态码
	switch st.Code() {
	case codes.Internal, StatusInternalServerError.status.Code():
		resp.response(c, http.StatusInternalServerError, strconv.Itoa(http.StatusInternalServerError), http.StatusText(http.StatusInternalServerError), nil)
		return true
	case codes.Unavailable, StatusServiceUnavailable.status.Code():
		resp.response(c, http.StatusServiceUnavailable, strconv.Itoa(http.StatusServiceUnavailable), http.StatusText(http.StatusServiceUnavailable), nil)
		return true
	}

	// 3. 检查是否包含强制转换为 HTTP Code 的标签
	if strings.Contains(st.Message(), ToHTTPCodeLabel) {
		code := convertToHTTPCode(st.Code())
		msg := strings.ReplaceAll(st.Message(), ToHTTPCodeLabel, "")
		resp.response(c, code, strconv.Itoa(int(st.Code())), msg, nil)
		return true
	}

	// 4. 检查用户自定义的 RPC 错误映射
	if resp.rpcStatus != nil && resp.isUserDefinedRPCErrorCode(c, int(st.Code())) {
		return true
	}

	// 5. 默认行为：响应 200 OK，Body 中包含错误码
	resp.response(c, http.StatusOK, strconv.Itoa(int(st.Code())), st.Message(), nil)
	return false
}

// handleHTTPError 处理来自 HTTP 的错误
func (resp *defaultResponse) handleHTTPError(c *gin.Context, err error) bool {
	e := ParseError(err)

	// 1. 处理标准 HTTP 错误
	switch e.Code() {
	case InternalServerError.Code(), http.StatusInternalServerError:
		resp.response(c, http.StatusInternalServerError, strconv.Itoa(http.StatusInternalServerError), http.StatusText(http.StatusInternalServerError), nil)
		return true
	case ServiceUnavailable.Code(), http.StatusServiceUnavailable:
		resp.response(c, http.StatusServiceUnavailable, strconv.Itoa(http.StatusServiceUnavailable), http.StatusText(http.StatusServiceUnavailable), nil)
		return true
	}

	// 2. 用户请求返回标准 HTTP 代码
	if e.needHTTPCode {
		msg := strings.ReplaceAll(e.msg, ToHTTPCodeLabel, "")
		resp.response(c, e.ToHTTPCode(), strconv.Itoa(e.code), msg, nil)
		return true
	}

	// 3. 检查用户自定义的 HTTP 错误映射
	if resp.httpErrors != nil && resp.isUserDefinedHTTPErrorCode(c, e.Code()) {
		return true
	}

	// 4. 默认行为
	resp.response(c, http.StatusOK, strconv.Itoa(e.code), e.msg, nil)
	return false
}

func (resp *defaultResponse) isUserDefinedRPCErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.rpcStatus[errCode]; ok {
		httpCode := ToHTTPErr(v.status).ToHTTPCode()
		msg := getStatusTextOrDefault(httpCode)
		resp.response(c, httpCode, strconv.Itoa(httpCode), msg, nil)
		return true
	}
	return false
}

func (resp *defaultResponse) isUserDefinedHTTPErrorCode(c *gin.Context, errCode int) bool {
	if v, ok := resp.httpErrors[errCode]; ok {
		httpCode := v.ToHTTPCode()
		msg := getStatusTextOrDefault(httpCode)
		resp.response(c, httpCode, strconv.Itoa(httpCode), msg, nil)
		return true
	}
	return false
}

// getStatusTextOrDefault 辅助函数，获取 HTTP 状态文本
func getStatusTextOrDefault(code int) string {
	msg := http.StatusText(code)
	if msg == "" {
		return "unknown error"
	}
	return msg
}

// ToHTTPErr 将 gRPC 状态转换为 HTTP 错误
func ToHTTPErr(st *status.Status) *Error { //nolint
	// 常见错误快速匹配
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
	}

	// 其他较少见的错误
	switch st.Code() {
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

	return &Error{
		code: int(st.Code()),
		msg:  st.Message(),
	}
}

// parseCodeAndMsg 解析错误字符串中的代码和消息
func parseCodeAndMsg(errStr string) (int, string) {
	if errStr == "" {
		return -1, ""
	}

	// 原始格式逻辑参考：ss := strings.Split(errStr, "desc = ")
	// 为了避免未使用的变量并保持逻辑严谨，我们直接定位最后一个 "desc = "
	descIdx := strings.LastIndex(errStr, "desc = ")
	if descIdx == -1 {
		return -1, errStr
	}

	// 提取 desc 之后的部分
	lastPart := errStr[descIdx+7:] // 7 是 "desc = " 的长度

	// 兼容原始逻辑中的 "msg = " 拆分
	cm := strings.Split(lastPart, "msg = ")
	if len(cm) == 2 {
		// 提取 code 部分：去掉前缀 "code = " 和后缀 ", "
		codeStr := cm[0]
		codeStr = strings.TrimPrefix(codeStr, "code = ")
		codeStr = strings.TrimSuffix(codeStr, ", ")

		code, err := strconv.Atoi(strings.TrimSpace(codeStr))
		if err == nil {
			return code, cm[1]
		}
	}

	return -1, errStr
}
