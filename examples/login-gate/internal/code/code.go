// Package code 定义 login-gate 示例的业务错误码。
//
// 框架自身的内置错误码在 github.com/gulurence/cherry-server/code 中(0~37)。
// 业务错误码从 200 段开始，避免与框架错误码冲突。
package code

const (
	OK int32 = 0 // 成功

	LoginError            int32 = 201 // 登录异常
	AccountExists         int32 = 202 // 账号已存在
	AccountPasswordError  int32 = 203 // 账号不存在或密码错误
	AccountNotLogin       int32 = 204 // 账号未登录
	AccountDuplicateLogin int32 = 205 // 账号重复登录(顶号)

	InvalidFaction int32 = 210 // 非法阵营
	TargetBlocked  int32 = 211 // 目标不可达或被阻挡
	NotEntered     int32 = 212 // 尚未进入世界
)

// IsOK 返回 code 是否为成功(0)。
func IsOK(code int32) bool {
	return code == OK
}

// IsFail 返回 code 是否为失败(非 0)。
func IsFail(code int32) bool {
	return code != OK
}
