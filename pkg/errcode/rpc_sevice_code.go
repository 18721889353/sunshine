package errcode

import "google.golang.org/grpc/codes"

// RCode 生成一个介于 400000 和 500000 之间的错误代码
//
// 该函数用于生成服务级别的错误代码，根据传入的数字生成相应的错误代码。
// 例如：
//
//	var (
//		StatusUserCreate = NewRPCStatus(RCode(1)+1, "failed to create user")		// 400101
//		StatusUserDelete = NewRPCStatus(RCode(1)+2, "failed to delete user")		// 400102
//		StatusUserUpdate = NewRPCStatus(RCode(1)+3, "failed to update user")		// 400103
//		StatusUserGet    = NewRPCStatus(RCode(1)+4, "failed to get user details")	// 400104
//	)
//
// 参数:
// - num: 生成错误代码的基础数字，必须在 1 到 999 之间。
//
// 返回:
// - codes.Code: 生成的 gRPC 错误代码。
func RCode(num int) codes.Code {
	if num > 999 || num < 1 {
		panic("范围必须在 1 到 999 之间")
	}
	return codes.Code(400000 + num*100)
}
