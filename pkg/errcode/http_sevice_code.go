package errcode

// HCode 根据传入的数字生成一个介于 200000 和 300000 之间的错误码
//
// 该函数用于生成 HTTP 服务级别的错误码，错误码前缀为 Err，例如：
//
// var (
//
//	ErrUserCreate = NewError(HCode(1)+1, "failed to create user")   // 200101
//	ErrUserDelete = NewError(HCode(1)+2, "failed to delete user")   // 200102
//	ErrUserUpdate = NewError(HCode(1)+3, "failed to update user")   // 200103
//	ErrUserGet    = NewError(HCode(1)+4, "failed to get user details") // 200104
//
// )
//
// 参数:
// - num: 一个整数，表示错误码的子类别编号，范围必须在 1 到 999 之间。
//
// 返回值:
// - 一个整数，表示生成的错误码。
//
// 异常:
// - 如果 num 不在 1 到 999 的范围内，函数会 panic 并抛出错误信息 "num range must be between 0 to 1000"。
func HCode(num int) int {
	// 检查 num 是否在有效范围内
	if num > 999 || num < 1 {
		panic("num range must be between 0 to 1000")
	}

	// 计算并返回错误码
	// 错误码的计算公式为：200000 + num * 100
	// 例如，num 为 1 时，返回 200100
	return 200000 + num*100
}
