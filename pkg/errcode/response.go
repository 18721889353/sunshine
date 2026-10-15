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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
	// Success 返回成功响应，code=0, msg="ok"，data 自动拆包；
	// 响应体字段名、默认 code/msg 内容、业务码提取模式均可通过 SetResponseFormat 注入（配合应用配置 response 段），无需替换实现
	Success(ctx *gin.Context, data any)
	// ParamError 参数错误响应（通常由 validation 校验失败触发）
	ParamError(ctx *gin.Context, err error)
	// Error 错误响应，根据错误类型自动切换 HTTP/RPC 处理，返回布尔值表示是否已处理
	Error(ctx *gin.Context, err error) bool
}

// ResponseFormat 全局响应格式配置，零值即内置默认行为；
// 由各应用的配置层（如 internal/config 的 response 段）在启动与热更新时通过 SetResponseFormat 注入，
// 每次响应时实时读取，天然支持热更新
type ResponseFormat struct {
	CodeField           string // 响应体 code 字段名，空则默认 "code"，可配为 status/resultCode 等
	MsgField            string // 响应体 msg 字段名，空则回退 isMessage 旧逻辑（msg/message），可配为 errorInfo 等
	DataField           string // 响应体 data 字段名，空则默认 "data"，可配为 result/payload 等
	SuccessCode         string // 成功响应码，空则默认 "0"
	SuccessMsg          string // 成功响应提示，空则默认 "ok"
	DoingMsg            string // 业务自定义码（非成功码）时的提示，空则回落 SuccessMsg，仅 ExtractCustomCode=true 时生效
	ParamErrorCode      string // 参数错误响应码，空则使用 InvalidParams 错误码
	ParamErrorMsg       string // 参数错误响应提示，空则使用 InvalidParams 提示
	ExtractCustomCode   bool   // 是否从 data 中提取 CustomCode（或 map 的 code 键）作为业务响应码，并从 data 中剥离该字段
	NilDataAsEmptyArray bool   // nil/空 data 序列化为 [] 而非 {}
}

// responseFormat 全局响应格式，未设置时为零值（默认行为）
var responseFormat atomic.Pointer[ResponseFormat]

// SetResponseFormat 设置全局响应格式，重复调用以最后一次为准（热更新场景由配置层重新调用）
func SetResponseFormat(f ResponseFormat) {
	responseFormat.Store(&f)
}

// currentFormat 读取当前全局响应格式快照
func currentFormat() ResponseFormat {
	if f := responseFormat.Load(); f != nil {
		return *f
	}
	return ResponseFormat{}
}

// orDefault 配置项空值回落到默认值
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// resolveMsgField 解析 msg 字段名：显式配置优先，未配置时回退 isMessage 旧逻辑保持兼容
func resolveMsgField(f ResponseFormat, isMessage bool) string {
	if f.MsgField != "" {
		return f.MsgField
	}
	if isMessage {
		return "message"
	}
	return "msg"
}

// emptyData nil data 的占位值：按配置输出 [] 或 {}
func emptyData(f ResponseFormat) any {
	if f.NilDataAsEmptyArray {
		return make([]any, 0)
	}
	return make(map[string]interface{}, 0)
}

// isNilData 判断 data 是否为「空数据」: 既包含未类型的 nil, 也包含带类型的 nil
// (如业务 handler 返回的 (nil *Reply) 装入 interface, 直接 == nil 判断为 false 会被序列化成 null)。
// 指针/接口/map/slice/func/chan 等引用类型需借 reflect.IsNil 才能识别真正的 nil。
func isNilData(data any) bool {
	if data == nil {
		return true
	}
	v := reflect.ValueOf(data)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return v.IsNil()
	}
	return false
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
//   - 按全局 ResponseFormat 解析响应体字段名（code/msg/data 均可自定义）
//   - 将 data=nil 或空切片按配置转为 {} 或 []，保证数据结构一致性
func (resp *defaultResponse) response(c *gin.Context, respStatus int, code, msg string, data any) {
	f := currentFormat()

	// 规范处理：错误响应或未设置数据(含带类型的 nil 指针, 如 handler 返回 (nil *Reply))时, data 按配置输出空对象 {} 或空数组 []
	if isNilData(data) {
		data = emptyData(f)
	} else if v := reflect.ValueOf(data); v.Kind() == reflect.Slice && v.Len() == 0 && !f.NilDataAsEmptyArray {
		// 非 nil 的空切片在关闭 NilDataAsEmptyArray 时仍回落为 {}, 保持既有语义
		data = make(map[string]interface{}, 0)
	}

	// 构建最终返回的 JSON 对象
	respData := make(map[string]any, 3)
	respData[orDefault(f.CodeField, "code")] = code
	respData[orDefault(f.DataField, "data")] = data
	respData[resolveMsgField(f, resp.isMessage)] = msg

	c.JSON(respStatus, respData)
}

// Success 成功响应，HTTP 状态码 200；
// 默认拆包 data 并以配置的 SuccessCode/SuccessMsg（未配置即 0/ok）输出；
// ExtractCustomCode=true 时从 data 中提取业务码，非成功码时使用 DoingMsg
func (resp *defaultResponse) Success(c *gin.Context, data any) {
	f := currentFormat()
	successCode := orDefault(f.SuccessCode, "0")
	successMsg := orDefault(f.SuccessMsg, "ok")

	if f.ExtractCustomCode {
		resp.successWithCustomCode(c, f, data, successCode, successMsg)
		return
	}

	resp.response(c, http.StatusOK, successCode, successMsg, resp.unwrapData(data))
}

// successWithCustomCode 业务码提取模式：从结构体 CustomCode 字段或 map 的 code/CustomCode 键中提取响应码，
// 提取后该字段从 data 中剥离；码与 SuccessCode 一致时用 SuccessMsg，否则用 DoingMsg
func (resp *defaultResponse) successWithCustomCode(c *gin.Context, f ResponseFormat, data any, successCode, successMsg string) {
	customCode := successCode
	processedData := data

	if data == nil {
		resp.response(c, http.StatusOK, customCode, successMsg, processedData)
		return
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct {
		if code, ok := extractCodeFromStruct(v); ok {
			customCode = code
		}
		processedData = structToMapWithoutCode(v)
	} else if v.Kind() == reflect.Map {
		if newData, code, ok := processMapData(v); ok {
			processedData = newData
			if code != "" {
				customCode = code
			}
		}
	}

	msg := successMsg
	if customCode != successCode {
		msg = orDefault(f.DoingMsg, successMsg)
	}
	resp.response(c, http.StatusOK, customCode, msg, processedData)
}

// extractCodeFromStruct 从结构体中提取 CustomCode 字段
func extractCodeFromStruct(v reflect.Value) (string, bool) {
	codeField := v.FieldByName("CustomCode")
	if !codeField.IsValid() || !codeField.CanInterface() {
		return "", false
	}

	switch codeField.Kind() {
	case reflect.String:
		if code := codeField.String(); code != "" {
			return code, true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(codeField.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(codeField.Uint(), 10), true
	}
	return "", false
}

// structToMapWithoutCode 将结构体转换为 map，排除 CustomCode 字段
func structToMapWithoutCode(v reflect.Value) map[string]interface{} {
	resultMap := make(map[string]interface{})
	structType := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldType := structType.Field(i)
		fieldValue := v.Field(i)

		if fieldType.IsExported() && fieldType.Name != "CustomCode" {
			jsonKey := string(fieldType.Name[0]+32) + fieldType.Name[1:]
			if fieldType.Name[0] >= 'A' && fieldType.Name[0] <= 'Z' {
				resultMap[jsonKey] = fieldValue.Interface()
			}
		}
	}
	return resultMap
}

// processMapData 处理 map 类型的数据，提取 code/CustomCode 键
func processMapData(v reflect.Value) (interface{}, string, bool) {
	if v.Type().Key().Kind() != reflect.String {
		return nil, "", false
	}

	newMap := reflect.MakeMap(v.Type())
	customCode := ""
	iter := v.MapRange()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()

		if key.String() != "code" && key.String() != "CustomCode" {
			newMap.SetMapIndex(key, value)
			continue
		}

		// map[string]any 等 interface 值类型需先解包再判断实际 Kind, 否则调用 Int()/String() 会 panic
		if value.Kind() == reflect.Interface {
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.String:
			if code := value.String(); code != "" {
				customCode = code
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			customCode = strconv.FormatInt(value.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			customCode = strconv.FormatUint(value.Uint(), 10)
		}
	}
	return newMap.Interface(), customCode, true
}

// respondWithHTTPCode 根据 HTTP 状态码生成标准响应（主要用于非 200 错误）
func (resp *defaultResponse) respondWithHTTPCode(c *gin.Context, httpCode int) {
	resp.response(c, httpCode, cast.ToString(httpCode), getStatusTextOrDefault(httpCode), nil)
}

// ParamError 参数错误响应，默认使用 errcode.InvalidParams 的错误码和消息，可通过 ResponseFormat 覆盖
func (resp *defaultResponse) ParamError(c *gin.Context, _ error) {
	f := currentFormat()
	code := orDefault(f.ParamErrorCode, cast.ToString(InvalidParams.Code()))
	msg := orDefault(f.ParamErrorMsg, InvalidParams.Msg())
	resp.response(c, http.StatusOK, code, msg, nil)
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
