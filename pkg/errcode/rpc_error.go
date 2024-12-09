package errcode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 存储 gRPC 错误代码及其对应的错误消息
var grpcErrCodes = map[int]string{}

// RPCStatus 用于封装 gRPC 状态
type RPCStatus struct {
	status *status.Status // 内部存储的 gRPC 状态
}

// 存储 gRPC 状态代码及其对应的错误消息
var statusCodes = map[codes.Code]string{}

// NewRPCStatus 创建一个新的 gRPC 状态
func NewRPCStatus(code codes.Code, msg string) *RPCStatus {
	if v, ok := statusCodes[code]; ok {
		panic(fmt.Sprintf(`gRPC 状态代码 %d 已经存在，请定义一个新的错误代码,
原始消息 = %s
新消息 = %s
`, code, v, msg))
	}

	grpcErrCodes[int(code)] = msg
	statusCodes[code] = msg
	return &RPCStatus{
		status: status.New(code, msg),
	}
}

// Detail 用于存储错误详情的键值对
type Detail struct {
	key string      // 键
	val interface{} // 值
}

// String 返回 Detail 的字符串表示形式
func (d *Detail) String() string {
	return fmt.Sprintf("%s: %v", d.key, d.val)
}

// Any 创建一个 Detail 对象
func Any(key string, val interface{}) Detail {
	return Detail{
		key: key,
		val: val,
	}
}

// Code 获取 gRPC 状态代码
func (s *RPCStatus) Code() codes.Code {
	return s.status.Code()
}

// Msg 获取 gRPC 状态消息
func (s *RPCStatus) Msg() string {
	return s.status.Message()
}

// Err 返回一个 gRPC 错误
// 如果有参数 'desc'，则替换原始消息
func (s *RPCStatus) Err(desc ...string) error {
	if len(desc) > 0 {
		return status.Errorf(s.status.Code(), "%s", strings.Join(desc, ", "))
	}
	return status.Errorf(s.status.Code(), "%s", s.status.Message())
}

// ErrToHTTP 将 gRPC 错误转换为标准 HTTP 错误，并添加 ToHTTPCodeLabel 标签
// 通常用于 HTTP 调用 gRPC API 时
// 如果有参数 'desc'，则替换原始消息
func (s *RPCStatus) ErrToHTTP(desc ...string) error {
	message := s.status.Message()
	if len(desc) > 0 {
		message = strings.Join(desc, ", ")
	}
	return status.Errorf(s.status.Code(), "%s%s", message, ToHTTPCodeLabel)
}

// ToRPCErr 将当前状态转换为标准的 gRPC 错误
// 如果有参数 'desc'，则替换原始消息
func (s *RPCStatus) ToRPCErr(desc ...string) error {
	switch s.status.Code() {
	case StatusInvalidParams.status.Code():
		return toRPCErr(codes.InvalidArgument, desc...)
	case StatusInternalServerError.status.Code():
		return toRPCErr(codes.Internal, desc...)
	}

	switch s.status.Code() {
	case StatusCanceled.status.Code():
		return toRPCErr(codes.Canceled, desc...)
	case StatusUnknown.status.Code():
		return toRPCErr(codes.Unknown, desc...)
	case StatusDeadlineExceeded.status.Code():
		return toRPCErr(codes.DeadlineExceeded, desc...)
	case StatusNotFound.status.Code():
		return toRPCErr(codes.NotFound, desc...)
	case StatusAlreadyExists.status.Code(), StatusConflict.status.Code():
		return toRPCErr(codes.AlreadyExists, desc...)
	case StatusPermissionDenied.status.Code():
		return toRPCErr(codes.PermissionDenied, desc...)
	case StatusResourceExhausted.status.Code():
		return toRPCErr(codes.ResourceExhausted, desc...)
	case StatusFailedPrecondition.status.Code():
		return toRPCErr(codes.FailedPrecondition, desc...)
	case StatusAborted.status.Code():
		return toRPCErr(codes.Aborted, desc...)
	case StatusOutOfRange.status.Code():
		return toRPCErr(codes.OutOfRange, desc...)
	case StatusUnimplemented.status.Code():
		return toRPCErr(codes.Unimplemented, desc...)
	case StatusServiceUnavailable.status.Code():
		return toRPCErr(codes.Unavailable, desc...)
	case StatusDataLoss.status.Code():
		return toRPCErr(codes.DataLoss, desc...)
	case StatusUnauthorized.status.Code():
		return toRPCErr(codes.Unauthenticated, desc...)
	case StatusAccessDenied.status.Code():
		return toRPCErr(codes.PermissionDenied, desc...)
	case StatusLimitExceed.status.Code():
		return toRPCErr(codes.ResourceExhausted, desc...)
	case StatusMethodNotAllowed.status.Code():
		return toRPCErr(codes.Unimplemented, desc...)
	}

	return s.status.Err()
}

// toRPCErr 创建一个标准的 gRPC 错误
func toRPCErr(code codes.Code, descs ...string) error {
	var desc string
	if len(descs) > 0 {
		desc = strings.Join(descs, ", ")
	} else {
		desc = code.String()
	}
	return status.New(code, desc).Err()
}

// ToRPCCode 将当前状态转换为标准的 gRPC 错误代码
func (s *RPCStatus) ToRPCCode() codes.Code {
	switch s.status.Code() {
	case StatusInvalidParams.status.Code():
		return codes.InvalidArgument
	case StatusInternalServerError.status.Code():
		return codes.Internal
	case StatusUnimplemented.status.Code():
		return codes.Unimplemented
	}

	switch s.status.Code() {
	case StatusPermissionDenied.status.Code():
		return codes.PermissionDenied
	case StatusCanceled.status.Code():
		return codes.Canceled
	case StatusUnknown.status.Code():
		return codes.Unknown
	case StatusDeadlineExceeded.status.Code():
		return codes.DeadlineExceeded
	case StatusNotFound.status.Code():
		return codes.NotFound
	case StatusAlreadyExists.status.Code(), StatusConflict.status.Code():
		return codes.AlreadyExists
	case StatusResourceExhausted.status.Code():
		return codes.ResourceExhausted
	case StatusFailedPrecondition.status.Code():
		return codes.FailedPrecondition
	case StatusAborted.status.Code():
		return codes.Aborted
	case StatusOutOfRange.status.Code():
		return codes.OutOfRange
	case StatusServiceUnavailable.status.Code():
		return codes.Unavailable
	case StatusDataLoss.status.Code():
		return codes.DataLoss
	case StatusUnauthorized.status.Code():
		return codes.Unauthenticated
	case StatusAccessDenied.status.Code():
		return codes.PermissionDenied
	case StatusLimitExceed.status.Code():
		return codes.ResourceExhausted
	case StatusMethodNotAllowed.status.Code():
		return codes.Unimplemented
	}

	return s.status.Code()
}

// convertToHTTPCode 将 gRPC 错误代码转换为 HTTP 状态代码
func convertToHTTPCode(code codes.Code) int {
	switch code {
	case StatusSuccess.status.Code():
		return http.StatusOK
	case codes.InvalidArgument, StatusInvalidParams.status.Code():
		return http.StatusBadRequest
	case codes.Internal, StatusInternalServerError.status.Code():
		return http.StatusInternalServerError
	case codes.Unimplemented, StatusUnimplemented.status.Code():
		return http.StatusNotImplemented
	case codes.NotFound, StatusNotFound.status.Code():
		return http.StatusNotFound
	case StatusForbidden.status.Code(), StatusAccessDenied.status.Code():
		return http.StatusForbidden
	}

	switch code {
	case StatusTimeout.status.Code():
		return http.StatusRequestTimeout
	case StatusTooManyRequests.status.Code(), StatusLimitExceed.status.Code():
		return http.StatusTooManyRequests
	case codes.FailedPrecondition, StatusFailedPrecondition.status.Code():
		return http.StatusPreconditionFailed
	case codes.Unavailable, StatusServiceUnavailable.status.Code():
		return http.StatusServiceUnavailable
	case codes.Unauthenticated, StatusUnauthorized.status.Code():
		return http.StatusUnauthorized
	case codes.PermissionDenied, StatusPermissionDenied.status.Code():
		return http.StatusUnauthorized
	case StatusLimitExceed.status.Code():
		return http.StatusTooManyRequests
	case StatusMethodNotAllowed.status.Code():
		return http.StatusMethodNotAllowed
	case StatusConflict.status.Code():
		return http.StatusConflict
	}

	return http.StatusInternalServerError
}

// GetStatusCode 从 gRPC 调用返回的错误中获取状态代码
func GetStatusCode(err error) codes.Code {
	st, _ := status.FromError(err)
	return st.Code()
}

// ErrInfo 用于存储错误信息
type ErrInfo struct {
	Code int    `json:"code"` // 错误代码
	Msg  string `json:"msg"`  // 错误消息
}

// getErrorInfo 获取所有错误代码及其对应的消息
func getErrorInfo(codeInfo map[int]string) []ErrInfo {
	var keys []int
	for key := range codeInfo {
		keys = append(keys, key)
	}

	sort.Ints(keys)
	eis := []ErrInfo{}
	for _, key := range keys {
		eis = append(eis, ErrInfo{
			Code: key,
			Msg:  codeInfo[key],
		})
	}

	return eis
}

// ListGRPCErrCodes 列出所有 gRPC 错误代码，HTTP 处理函数
func ListGRPCErrCodes(w http.ResponseWriter, _ *http.Request) {
	eis := getErrorInfo(grpcErrCodes)

	jsonData, err := json.Marshal(&eis)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(jsonData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// ShowConfig 显示配置信息
// @Summary 显示配置信息
// @Description 显示配置信息
// @Tags system
// @Accept  json
// @Produce  json
// @Router /config [get]
func ShowConfig(jsonData []byte) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		//w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(jsonData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
