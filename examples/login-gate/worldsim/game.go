package worldsim

import (
	"math/rand"
	"sort"
	"time"
)

// MaxWanderExpand 限制 NPC 游走单次 A* 的展开节点数：对“被河流/山隔断的
// 目标”不会再展开整个连通域，而是尽早判定不可达，避免 3000 个 NPC 时卡顿。
const MaxWanderExpand = 30000

// repairPathRange 限制城堡新增时的反向修正只覆盖对象前方该格数内的路点。距离还很远
// 的城堡（对象很久才走到、期间还可能被 churn 删掉）不立即重寻路，等对象走近时由
// Update 的被动绕行（下一格被挡即重寻）兜底，避免为“远未到达”的城堡浪费全图 A*。
const repairPathRange = 50

// repathReq 是一次延迟重寻路请求。
type repathReq struct {
	Obj    *Object
	Target Vec2i
}

// repathPending 是一次已入队、等待 worker 结果的异步反向修正重寻路。
// target 记录入队时的目标，用于在结果返回时丢弃“对象目标已变”的过期结果。
type repathPending struct {
	o      *Object
	target Vec2i
}

// Game 管理世界内的所有对象与每帧推进。
// 单线程使用：由 world actor 的 tick 串行调用 Update。
type Game struct {
	grid *Grid
	hash *SpatialHash
	objs map[int]*Object
	rng  *rand.Rand
	pf   *Pathfinder
	pool *PathPool

	// 动态城堡（不可通行阻挡）：由 UpdateCastles 周期性增删，供客户端预寻路后的修正。
	castles        []*Castle
	nextCastleID   int
	castleTarget   int     // SeedCastles 实际放置的数量，UpdateCastles 据此维持覆盖并增删
	castleSlots    []Vec2i // 城堡槽位（左上角，已洗牌），AddCastle 顺序扫描
	castleSlotIdx  int     // 下次放置时从哪个槽位开始扫描
	lastCastleTick time.Time

	lastCollision map[uint64]time.Time
	repathQueue   []repathReq

	// 反向修正重寻路：城堡新增时，穿过新城堡格子的玩家走 worker 池异步重寻路到原目标。
	repaths         map[int64]repathPending
	nextRepathToken int64

	// OnCollision 在发生碰撞时回调（阵营关系已判定）。由上层注入日志等。
	OnCollision func(a, b *Object, rel Relation)
	// OnPathChanged 在对象获得新路径时回调（含寻路耗时）。由上层注入路径推送。
	OnPathChanged func(o *Object)
}

// NewGame 创建一个绑定到 grid 的模拟世界。pathWorkers 是玩家 moveTo 寻路 worker 池大小。
func NewGame(grid *Grid, rng *rand.Rand, pathWorkers int) *Game {
	g := &Game{
		grid:           grid,
		hash:           NewSpatialHash(),
		objs:           make(map[int]*Object),
		rng:            rng,
		pf:             NewPathfinder(),
		pool:           NewPathPool(pathWorkers),
		lastCastleTick: time.Now(),
		lastCollision:  make(map[uint64]time.Time),
	}
	g.pool.Refresh(grid.walkable)
	return g
}

func pairKey(a, b int) uint64 {
	if a > b {
		a, b = b, a
	}
	return uint64(a)<<32 | uint64(b)
}

// Grid 返回底层网格（供上层判断格子可通行 / 随机出生点）。
func (g *Game) Grid() *Grid { return g.grid }

// PathPool 返回寻路 worker 池（供上层提交玩家 moveTo 的异步全图寻路并回填结果）。
func (g *Game) PathPool() *PathPool { return g.pool }

// SubmitPath 向 worker 池提交一次寻路。由 world actor 串行调用。
func (g *Game) SubmitPath(job PathJob) { g.pool.Submit(job) }

// refreshSnapshot 把 grid 当前可通行状态整体刷新进 worker 池快照，供 worker 只读。
// 在城堡增删（SeedCastles / UpdateCastles）之后调用。
func (g *Game) refreshSnapshot() { g.pool.Refresh(g.grid.walkable) }

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
		if o.Size > 0 {
			o.Radius = RadiusForSize(o.Size)
		} else {
			o.Radius = 0.4
		}
	}
	if o.Speed <= 0 {
		o.Speed = MoveSpeed
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
// 会清空旧路径并重置到 StateMoving。玩家 moveTo 走不设上限的全图寻路（maxExpand=0）。
func (g *Game) SetPath(o *Object, target Vec2i) []Vec2i {
	start := time.Now()
	path := g.pf.Find(g.grid, o.Cell, target, 0)
	o.PathCostMs = time.Since(start).Milliseconds()
	if path == nil {
		return nil
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = target
	o.State = StateMoving
	if g.OnPathChanged != nil {
		g.OnPathChanged(o)
	}
	return path
}

// SetClientPath 采用客户端预寻路结果。客户端只知道静态地形、不知道动态城堡，因此其
// 路径可能穿过新建的城堡；这里对路径做两件事：
//  1. rebasePath 对齐对象当前格；
//  2. repathBlocked 把穿过阻挡格（动态城堡）的连续段用服务器 A* 绕行修正。
//
// 修正后整体合法则直接采用（未穿过城堡时几乎零开销）；不可达或目标不匹配时回退
// 服务器全量寻路。返回最终采用的路径（nil 表示不可达）。
func (g *Game) SetClientPath(o *Object, target Vec2i, clientPath []Vec2i) []Vec2i {
	path := RebasePath(o.Cell, clientPath)
	if path == nil || len(path) == 0 || path[len(path)-1] != target {
		return g.SetPath(o, target)
	}

	start := time.Now()
	fixed, ok := g.repathBlocked(path)
	cost := time.Since(start).Milliseconds()
	if !ok || ValidatePath(g.grid, fixed) != len(fixed) {
		return g.SetPath(o, target)
	}

	o.Path = fixed
	o.PathIdx = 0
	o.Target = target
	o.State = StateMoving
	o.PathCostMs = cost // 未穿城堡≈0；穿城堡时为绕行 A* 耗时，其余显示由客户端本地 A* 给出
	if g.OnPathChanged != nil {
		g.OnPathChanged(o)
	}
	return fixed
}

// repathBlocked 扫描 path，把穿过阻挡格（动态城堡）的连续段用服务器 A* 绕行。
// 返回修正后的完整路径；任一段不可达（如目标被城堡覆盖）时返回 nil,false。
func (g *Game) repathBlocked(path []Vec2i) ([]Vec2i, bool) {
	out := make([]Vec2i, 0, len(path))
	i := 0
	for i < len(path) {
		if g.grid.Walkable(path[i]) {
			out = append(out, path[i])
			i++
			continue
		}
		// path[i] 被阻挡：找到其前一个合法格 start 与后一个合法格 end
		if i == 0 {
			return nil, false // 起点即阻挡（rebase 后 path[0] 应为对象当前格，不应发生）
		}
		start := out[len(out)-1] // = path[i-1]，已入 out
		for i < len(path) && !g.grid.Walkable(path[i]) {
			i++
		}
		if i >= len(path) {
			return nil, false // 阻挡延续到终点：目标被城堡覆盖
		}
		end := path[i]
		seg := g.pf.Find(g.grid, start, end, 0)
		if seg == nil {
			return nil, false
		}
		out = append(out, seg[1:]...) // seg[0]==start，跳过避免重复
		i++                            // end 已由 seg 补入 out，继续处理其后
	}
	return out, true
}

// SetPathLimited 与 SetPath 相同，但用有节点上限的 A*（见 MaxWanderExpand）。
func (g *Game) SetPathLimited(o *Object, target Vec2i, maxExpand int) []Vec2i {
	start := time.Now()
	path := g.pf.Find(g.grid, o.Cell, target, maxExpand)
	o.PathCostMs = time.Since(start).Milliseconds()
	if path == nil {
		return nil
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = target
	o.State = StateMoving
	if g.OnPathChanged != nil {
		g.OnPathChanged(o)
	}
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
		if g.grid.Walkable(t) && g.SetPathLimited(o, t, MaxWanderExpand) != nil {
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
			// 路径前方 repairPathRange 格内有格被动态城堡覆盖 → 重寻路绕行（或目标被压则停下）。
			// 只扫“一定范围”：更远的城堡先不修，等对象走近到范围内再触发（避免为远未到达、
			// 可能被 churn 删掉的城堡浪费全图 A*）。下一格被挡时本帧不能前进；更远则继续走，
			// 重寻路结果回来后再换路径。
			if o.PathIdx < len(o.Path) {
				if d := g.firstBlockedAhead(o, repairPathRange); d >= 0 {
					if !o.RepathPending {
						g.repathOrClear(o)
					}
					if d == 0 {
						continue
					}
				}
			}
			to, toCell, ok := o.nextStep(dt)
			if !ok {
				continue
			}
			intents = append(intents, intent{o, o.Pos, to, toCell})
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
}

func (g *Game) enqueueRepath(o *Object) {
	o.RepathPending = true
	g.repathQueue = append(g.repathQueue, repathReq{o, o.Target})
}

// firstBlockedAhead 返回对象剩余路径中第一个非法步相对 PathIdx 的偏移（0 表示下一格，
// 1 表示再下一格……），前方 r 格内都合法则返回 -1。非法包含两类：格子被动态城堡覆盖、
// 以及斜向步因新城堡落在正交邻居上而“穿墙角”。用于城堡新增后按“一定范围”反向修正：
// 只对走进范围内的对象触发，范围外的城堡不动。
func (g *Game) firstBlockedAhead(o *Object, r int) int {
	end := o.PathIdx + r
	if end > len(o.Path) {
		end = len(o.Path)
	}
	// 从对象当前格起逐格校验（含当前正在走的那一步），与 ValidatePath 规则一致。
	prev := o.Cell
	for i := o.PathIdx; i < end; i++ {
		c := o.Path[i]
		if !g.grid.Walkable(c) {
			return i - o.PathIdx
		}
		dx, dy := c.X-prev.X, c.Y-prev.Y
		if dx != 0 && dy != 0 {
			// 斜向不穿墙角：两个正交邻居都必须可走（可能被新城堡新压住）。
			if !g.grid.Walkable(Vec2i{prev.X + dx, prev.Y}) ||
				!g.grid.Walkable(Vec2i{prev.X, prev.Y + dy}) {
				return i - o.PathIdx
			}
		}
		prev = c
	}
	return -1
}

// repathOrClear 处理对象路径被动态城堡阻挡：目标本身被压则清空路径停下（NPC 稍后
// 由 WanderNPCs 换新目标），否则玩家走 worker 池异步全图重寻、NPC 进入分帧重寻队列。
func (g *Game) repathOrClear(o *Object) {
	if !g.grid.Walkable(o.Target) {
		o.Path = nil
		o.PathIdx = 0
		return
	}
	if o.IsPlayer {
		g.RepathAsync(o)
	} else {
		g.enqueueRepath(o)
	}
}

// RepathAsync 为对象异步重寻路到其当前 Target（走 worker 池，避免全图 A* 阻塞 actor）。
// 结果由 ApplyRepathResult 回填（仍在 actor goroutine）。
func (g *Game) RepathAsync(o *Object) {
	o.RepathPending = true
	token := g.nextRepathToken
	g.nextRepathToken++
	if g.repaths == nil {
		g.repaths = make(map[int64]repathPending)
	}
	g.repaths[token] = repathPending{o: o, target: o.Target}
	g.pool.Submit(PathJob{Token: token, Start: o.Cell, Goal: o.Target, MaxExpand: 0, Kind: PathRepath})
}

// ApplyRepathResult 回填一次异步反向修正重寻路的结果到对象。由上层在 drainPathResults 里
// 对 Kind==PathRepath 的结果调用（actor goroutine）。
func (g *Game) ApplyRepathResult(r PathResult) {
	rp, ok := g.repaths[r.Token]
	if !ok {
		return
	}
	delete(g.repaths, r.Token)
	o := rp.o
	o.RepathPending = false
	if g.objs[o.ID] != o || o.Target != rp.target {
		return // 对象已移除或目标已变（如玩家又发了新 moveTo），丢弃过期结果
	}
	if r.Path == nil {
		// 目标在重寻路期间被覆盖 / 不可达：停下。
		o.Path = nil
		o.PathIdx = 0
		return
	}
	path := RebasePath(o.Cell, r.Path)
	if path == nil {
		// 对象在寻路期间移动过多：以当前格重新入队。
		g.RepathAsync(o)
		return
	}
	o.Path = path
	o.PathIdx = 0
	o.State = StateMoving
	o.PathCostMs = r.CostMs
	if g.OnPathChanged != nil {
		g.OnPathChanged(o)
	}
}

func (g *Game) runPathBudget(d time.Duration) {
	deadline := time.Now().Add(d)
	for len(g.repathQueue) > 0 && time.Now().Before(deadline) {
		req := g.repathQueue[0]
		g.repathQueue = g.repathQueue[1:]
		req.Obj.RepathPending = false
		start := time.Now()
		path := g.pf.Find(g.grid, req.Obj.Cell, req.Target, MaxWanderExpand)
		req.Obj.PathCostMs = time.Since(start).Milliseconds()
		if len(path) > 0 {
			req.Obj.Path = path
			req.Obj.PathIdx = 0
			if g.OnPathChanged != nil {
				g.OnPathChanged(req.Obj)
			}
		} else if !g.grid.Walkable(req.Target) {
			// 目标被动态城堡覆盖：清空路径停下，NPC 由 WanderNPCs 换新目标。
			req.Obj.Path = nil
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

			// 敌对/友方/中立一律直接通过（不改变状态）：红黑相遇不再停止移动。
			break
		}
	}
}

// WanderNPCs 为到达目的地 / 空闲的 NPC 随机挑选新目标并寻路。
// 每个 NPC 受 WanderCooldown 节流：未到时间则跳过，避免大量 NPC 在同一帧
// 同时触发 A*（尤其是一直找不到可达目标的 NPC，会退化成每帧一次重寻路）。
func (g *Game) WanderNPCs(now time.Time) {
	for _, o := range g.objs {
		if o.IsPlayer {
			continue
		}
		if o.State != StateMoving || o.PathIdx < len(o.Path) {
			continue
		}
		if now.Before(o.WanderAt) {
			continue
		}
		o.WanderAt = now.Add(WanderCooldown)
		g.AssignRandomTarget(o)
	}
}
