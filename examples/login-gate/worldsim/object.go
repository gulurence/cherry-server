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
	MoveSpeed         = 4.0               // 移动速度（格/秒）
	BlockTimeout      = 3 * time.Second   // 被阻挡后强制重规划的时间
	CollisionCooldown = 200 * time.Millisecond
	MaxRadius         = 1.0
)

// Object 世界中的可移动对象（玩家与 NPC 共用）。
type Object struct {
	ID       int
	Faction  Faction
	IsPlayer bool
	Uid      int64 // 玩家 uid；NPC 为 0

	Pos    Vec2   // 连续坐标
	Cell   Vec2i  // 当前所在格子
	Radius float32

	Path    []Vec2i
	PathIdx int
	State   ObjState

	BlockSince    time.Time
	RequestRepath bool
	Target        Vec2i
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

	step := MoveSpeed * dt
	if step > d {
		step = d
	}
	newPos := Vec2{o.Pos.X + dir.X/d*step, o.Pos.Y + dir.Y/d*step}
	newCell := Vec2i{int(newPos.X), int(newPos.Y)}

	return newPos, newCell, true
}
