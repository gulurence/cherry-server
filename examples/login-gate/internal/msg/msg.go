// Package msg 定义 login-gate 示例在客户端与节点之间、节点与节点之间传递的消息结构。
//
// 示例统一使用 JSON 序列化(见各节点的 Run())，因此消息是普通 Go 结构体，
// 不需要 protobuf 代码生成步骤。字段名上的 json tag 即线上的字段名。
package msg

// LoginRequest 登录请求。它同时承担两个角色：
//
//	1. 客户端 -> 网关   (route: gate.user.login 的请求体)
//	2. 网关   -> 登录服 (RPC 调用 login 节点 account.verify 的参数)
type LoginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

// LoginResponse 登录响应。同样承担两个角色：
//
//	1. 登录服 -> 网关   (account.verify 的返回值)
//	2. 网关   -> 客户端 (gate.user.login 的响应体)
type LoginResponse struct {
	UID   int64  `json:"uid"`
	Token string `json:"token"`
}

// Int32 是通用整型消息，用于错误码、踢人原因等场景。
type Int32 struct {
	Value int32 `json:"value"`
}

// EmptyRequest 空请求，供暂无字段的占位协议复用。
type EmptyRequest struct{}

// WorldInfo world actor 返回的世界信息(骨架占位)。
type WorldInfo struct {
	WorldID string `json:"world_id"`
}

// PlayerInfo player actor 返回的玩家信息(骨架占位)。
type PlayerInfo struct {
	UID int64 `json:"uid"`
}
