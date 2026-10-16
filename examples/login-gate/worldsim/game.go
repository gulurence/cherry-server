package worldsim

import (
	"math/rand"
	"sort"
	"time"
)

// repathReq 是一次延迟重寻路请求。
type repathReq struct {
	Obj    *Object
	Target Vec2i
}

// Game 管理世界内的所有对象与每帧推进。
// 单线程使用：由 world actor 的 tick 串行调用 Update。
type Game struct {
	grid *Grid
	hash *SpatialHash
	objs map[int]*Object
	rng  *rand.Rand

	lastCollision map[uint64]time.Time
	repathQueue   []repathReq

	// OnCollision 在发生碰撞时回调（阵营关系已判定）。由上层注入日志等。
	OnCollision func(a, b *Object, rel Relation)
}

// NewGame 创建一个绑定到 grid 的模拟世界。
func NewGame(grid *Grid, rng *rand.Rand) *Game {
	return &Game{
		grid:          grid,
		hash:          NewSpatialHash(),
		objs:          make(map[int]*Object),
		rng:           rng,
		lastCollision: make(map[uint64]time.Time),
	}
}

func pairKey(a, b int) uint64 {
	if a > b {
		a, b = b, a
	}
	return uint64(a)<<32 | uint64(b)
}

// Grid 返回底层网格（供上层判断格子可通行 / 随机出生点）。
func (g *Game) Grid() *Grid { return g.grid }

// Object 按 id 返回对象，不存在返回 nil。
func (g *Game) Object(id int) *Object { return g.objs[id] }

// Objects 返回当前所有对象（调用方只读，不要并发访问）。
func (g *Game) Objects() []*Object {
	out := make([]*Object, 0, len(g.objs))
	for _, o := range g.objs {
		out = append(out, o)
	}
	return out
}

// AddObject 在对象当前位置放置它（不自动寻路）。
func (g *Game) AddObject(o *Object) {
	if o.Radius <= 0 {
		o.Radius = 0.4
	}
	o.Cell = Vec2i{int(o.Pos.X), int(o.Pos.Y)}
	o.State = StateMoving
	g.objs[o.ID] = o
	g.hash.Insert(o)
}

// RemoveObject 移除对象。
func (g *Game) RemoveObject(id int) {
	o := g.objs[id]
	if o == nil {
		return
	}
	g.hash.Remove(o)
	delete(g.objs, id)
}

// SetPath 为对象设定一条到 target 的路径，返回该路径（nil 表示不可达）。
// 会清空旧路径并重置到 StateMoving。
func (g *Game) SetPath(o *Object, target Vec2i) []Vec2i {
	path := AStar(g.grid, o.Cell, target)
	if path == nil {
		return nil
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = target
	o.State = StateMoving
	o.RequestRepath = false
	return path
}

// AssignRandomTarget 为 NPC 在当前位置附近随机挑一个可通行目标并寻路。
func (g *Game) AssignRandomTarget(o *Object) {
	const sampleRange = 80
	const maxTry = 32

	for i := 0; i < maxTry; i++ {
		tx := o.Cell.X + g.rng.Intn(sampleRange*2+1) - sampleRange
		ty := o.Cell.Y + g.rng.Intn(sampleRange*2+1) - sampleRange
		t := Vec2i{tx, ty}
		if g.grid.Walkable(t) {
			g.SetPath(o, t)
			return
		}
	}
}

// Update 推进一帧。
func (g *Game) Update(dt float32) {
	now := time.Now()

	// 1. 分帧寻路（限时预算，避免长路径阻塞调用方）
	g.runPathBudget(2 * time.Millisecond)

	// 2. 收集意图
	var intents []intent

	for _, o := range g.objs {
		switch o.State {
		case StateMoving:
			if o.RequestRepath {
				o.RequestRepath = false
				g.enqueueRepath(o)
				continue
			}
			to, toCell, ok := o.nextStep(dt)
			if !ok {
				continue
			}
			intents = append(intents, intent{o, o.Pos, to, toCell})
		case StateBlocked:
			g.checkUnblock(o, now)
		}
	}

	// 3. 固定顺序（按 id），保证确定性
	sort.Slice(intents, func(i, j int) bool {
		return intents[i].obj.ID < intents[j].obj.ID
	})

	// 4. 碰撞检测 + 同步判定
	g.resolveIntents(intents, now)

	// 5. 提交位置（仅仍为 StateMoving 的对象）
	for _, it := range intents {
		if it.obj.State == StateMoving {
			it.obj.Pos = it.to
			g.hash.Move(it.obj, it.toCell)
		}
	}

	// 6. 死锁检测
	g.detectDeadlock(now)
}

func (g *Game) enqueueRepath(o *Object) {
	g.repathQueue = append(g.repathQueue, repathReq{o, o.Target})
}

func (g *Game) runPathBudget(d time.Duration) {
	deadline := time.Now().Add(d)
	for len(g.repathQueue) > 0 && time.Now().Before(deadline) {
		req := g.repathQueue[0]
		g.repathQueue = g.repathQueue[1:]
		path := AStar(g.grid, req.Obj.Cell, req.Target)
		if len(path) > 0 {
			req.Obj.Path = path
			req.Obj.PathIdx = 0
		}
	}
}

// intent 表示一个对象本帧的移动意图。
type intent struct {
	obj    *Object
	from   Vec2
	to     Vec2
	toCell Vec2i
}

func (g *Game) resolveIntents(intents []intent, now time.Time) {
	seen := make(map[uint64]bool)

	for i := range intents {
		it := &intents[i]
		if it.obj.State != StateMoving {
			continue
		}

		neighbors := g.hash.QueryCircle(it.to, it.obj.Radius+MaxRadius)
		for _, other := range neighbors {
			if other.ID == it.obj.ID {
				continue
			}

			rr := it.obj.Radius + other.Radius
			if it.to.distSq(other.Pos) >= rr*rr {
				continue
			}

			key := pairKey(it.obj.ID, other.ID)
			if seen[key] {
				continue
			}
			seen[key] = true

			if t, ok := g.lastCollision[key]; ok && now.Sub(t) < CollisionCooldown {
				continue
			}
			g.lastCollision[key] = now

			rel := RelationOf(it.obj.Faction, other.Faction)
			if g.OnCollision != nil {
				g.OnCollision(it.obj, other, rel)
			}

			if rel == RelEnemy {
				it.obj.State = StateBlocked
				it.obj.BlockSince = now
				it.obj.RequestRepath = true
			}
			// 友方/中立：直接通过（不改变状态）
			break
		}
	}
}

func (g *Game) checkUnblock(o *Object, now time.Time) {
	neighbors := g.hash.QueryCircle(o.Pos, o.Radius+MaxRadius)
	blocked := false
	for _, other := range neighbors {
		if other.ID == o.ID {
			continue
		}
		rr := o.Radius + other.Radius
		if o.Pos.distSq(other.Pos) < rr*rr {
			blocked = true
			break
		}
	}
	if !blocked {
		o.State = StateMoving
		return
	}
	if now.Sub(o.BlockSince) > BlockTimeout {
		o.State = StateMoving
		o.RequestRepath = true
	}
}

// detectDeadlock 所有被阻挡超过阈值的对象互相等待时，随机放行一个。
func (g *Game) detectDeadlock(now time.Time) {
	var blocked []*Object
	for _, o := range g.objs {
		if o.State == StateBlocked && now.Sub(o.BlockSince) > BlockTimeout {
			blocked = append(blocked, o)
		}
	}
	if len(blocked) >= 2 {
		victim := blocked[g.rng.Intn(len(blocked))]
		victim.State = StateMoving
		victim.RequestRepath = true
	}
}

// WanderNPCs 为到达目的地 / 空闲的 NPC 随机挑选新目标并寻路。
func (g *Game) WanderNPCs() {
	for _, o := range g.objs {
		if o.IsPlayer {
			continue
		}
		if o.State != StateMoving || o.PathIdx < len(o.Path) {
			continue
		}
		g.AssignRandomTarget(o)
	}
}
