package worldsim

import (
	"math"
	"time"
)

// ObjState 对象状态机状态。
type ObjState int

const (
	StateMoving  ObjState = iota // 沿路径移动
	StateBlocked                 // 被敌对对象阻挡
)

const (
	MoveSpeed         = 2.0               // NPC 移动速度（格/秒），已减慢一倍
	PlayerMoveSpeed   = 4.0               // 玩家移动速度（格/秒），比 NPC 快两倍
	BlockTimeout      = 5 * time.Second   // 敌人相遇后，等待 5 秒再强制重新寻路移动
	CollisionCooldown = 200 * time.Millisecond
	MaxRadius         = 1.5 // 最大对象半径（尺寸 3 对应 1.5），作为空间查询的扩展半径
	WanderCooldown    = time.Second // NPC 空闲后重新挑目标的最小间隔，避免大量 NPC 同时 A*
)

// RadiusForSize 返回尺寸 size（格）对应的碰撞半径（半格宽）。
// 尺寸 1/2/3 分别对应半径 0.5/1.0/1.5。
func RadiusForSize(size int) float32 {
	if size < 1 {
		size = 1
	}
	return float32(size) * 0.5
}

// Object 世界中的可移动对象（玩家与 NPC 共用）。
type Object struct {
	ID       int
	Faction  Faction
	IsPlayer bool
	Uid      int64 // 玩家 uid；NPC 为 0

	Pos    Vec2   // 连续坐标
	Cell   Vec2i  // 当前所在格子
	Radius float32
	Size   int     // 尺寸（格）：1/2/3，决定碰撞半径与客户端绘制大小
	Speed  float32 // 移动速度（格/秒）；0 表示用默认 MoveSpeed

	Path    []Vec2i
	PathIdx int
	State   ObjState

	BlockSince    time.Time
	RequestRepath bool
	Target        Vec2i
	PathCostMs    int64     // 最近一次寻路耗时（毫秒）
	WanderAt      time.Time // 下次允许随机挑新目标的时刻（节流 NPC 重寻路）
}

// nextStep 计算沿 Path 前进 dt 秒后的下一个位置。
// 返回 (新连续位置, 新格子, 是否仍有路径可走)。
func (o *Object) nextStep(dt float32) (Vec2, Vec2i, bool) {
	if o.PathIdx >= len(o.Path) {
		return Vec2{}, Vec2i{}, false
	}
	target := o.Path[o.PathIdx]
	goalPos := Vec2{float32(target.X) + 0.5, float32(target.Y) + 0.5}

	dir := Vec2{goalPos.X - o.Pos.X, goalPos.Y - o.Pos.Y}
	d := float32(math.Sqrt(float64(dir.X*dir.X + dir.Y*dir.Y)))
	if d < 0.05 {
		o.PathIdx++
		if o.PathIdx >= len(o.Path) {
			return Vec2{}, Vec2i{}, false
		}
		target = o.Path[o.PathIdx]
		goalPos = Vec2{float32(target.X) + 0.5, float32(target.Y) + 0.5}
		dir = Vec2{goalPos.X - o.Pos.X, goalPos.Y - o.Pos.Y}
		d = float32(math.Sqrt(float64(dir.X*dir.X + dir.Y*dir.Y)))
	}

	step := o.Speed * dt
	if step > d {
		step = d
	}
	newPos := Vec2{o.Pos.X + dir.X/d*step, o.Pos.Y + dir.Y/d*step}
	newCell := Vec2i{int(newPos.X), int(newPos.Y)}

	return newPos, newCell, true
}
