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

// Cell 世界格子坐标。
type Cell struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// WorldObject 世界对象（objEnter 推送与进入世界快照的元素）。
type WorldObject struct {
	ID      int64   `json:"id"`
	Kind    int32   `json:"kind"`    // 0=NPC, 1=玩家
	Faction int32   `json:"faction"` // 0红 1黑 2绿
	Size    int32   `json:"size"`    // 尺寸（格）：1/2/3
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	CellX   int32   `json:"cell_x"`
	CellY   int32   `json:"cell_y"`
}

// EnterRequest 进入世界请求（携带所选阵营）。
type EnterRequest struct {
	Faction int32 `json:"faction"`
}

// EnterResponse 进入世界响应：地图 + 出生点 + 初始可见对象快照。
type EnterResponse struct {
	Map     string        `json:"map"`     // 1,000,000 个字符 '0'/'1'/'2'
	SelfID  int64         `json:"self_id"` // 自己的对象 id
	SpawnX  int32         `json:"spawn_x"`
	SpawnY  int32         `json:"spawn_y"`
	Faction int32         `json:"faction"`
	Objects []WorldObject `json:"objects"`
}

// MoveToRequest 移动到目标格子。
type MoveToRequest struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// MoveToResponse 移动请求响应（含 A* 路径与寻路耗时）。
type MoveToResponse struct {
	Code   int32  `json:"code"`     // 0 成功
	CostMs int32  `json:"cost_ms"`  // 寻路耗时（毫秒）
	Path   []Cell `json:"path"`
}

// ViewRequest 上报视野中心（客户端当前显示中心）。
type ViewRequest struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// ViewResponse 视野上报响应。
type ViewResponse struct {
	Code int32 `json:"code"`
}

// ObjMovePush 对象移动推送。
type ObjMovePush struct {
	ID    int64   `json:"id"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	CellX int32   `json:"cell_x"`
	CellY int32   `json:"cell_y"`
}

// ObjLeavePush 对象离开视野推送。
type ObjLeavePush struct {
	ID int64 `json:"id"`
}

// ObjPathPush 对象寻路结果推送：路点线条 + 寻路耗时（毫秒）。
type ObjPathPush struct {
	ID     int64  `json:"id"`
	CostMs int32  `json:"cost_ms"`
	Path   []Cell `json:"path"`
}
