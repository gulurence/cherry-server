// main.go
package main

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// ============================================================
// 基础类型
// ============================================================

type Vec2 struct{ X, Y float32 }
type Vec2i struct{ X, Y int }

func (a Vec2) distSq(b Vec2) float32 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

// ============================================================
// 1. 静态网格 + A* + JPS
// ============================================================

const (
	MapW, MapH = 1000, 1000
)

type Grid struct {
	walkable []bool // 1D 数组，省内存
	cost     []int  // 软代价
}

func NewGrid() *Grid {
	g := &Grid{
		walkable: make([]bool, MapW*MapH),
		cost:     make([]int, MapW*MapH),
	}
	for i := range g.walkable {
		g.walkable[i] = true
	}
	return g
}

func (g *Grid) idx(c Vec2i) int { return c.Y*MapW + c.X }

func (g *Grid) InBounds(c Vec2i) bool {
	return c.X >= 0 && c.X < MapW && c.Y >= 0 && c.Y < MapH
}

func (g *Grid) Walkable(c Vec2i) bool {
	return g.InBounds(c) && g.walkable[g.idx(c)]
}

func (g *Grid) SetBlocked(c Vec2i, blocked bool) {
	if g.InBounds(c) {
		g.walkable[g.idx(c)] = !blocked
	}
}

func (g *Grid) CellCost(c Vec2i) int {
	if !g.InBounds(c) {
		return math.MaxInt32
	}
	return 1 + g.cost[g.idx(c)]
}

// ---------- A*（8 方向，Octile 启发） ----------

type astarNode struct {
	cell   Vec2i
	g      int
	f      int
	parent *astarNode
	index  int // heap 索引
}

type nodeHeap []*astarNode

func (h nodeHeap) Len() int            { return len(h) }
func (h nodeHeap) Less(i, j int) bool  { return h[i].f < h[j].f }
func (h nodeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *nodeHeap) Push(x interface{}) { n := x.(*astarNode); n.index = len(*h); *h = append(*h, n) }
func (h *nodeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return it
}

var (
	dx8 = [8]int{1, -1, 0, 0, 1, 1, -1, -1}
	dy8 = [8]int{0, 0, 1, -1, 1, -1, 1, -1}
)

func octile(a, b Vec2i) int {
	dx := abs(a.X - b.X)
	dy := abs(a.Y - b.Y)
	if dx > dy {
		return 10*(dx-dy) + 14*dy
	}
	return 10*(dy-dx) + 14*dx
}

func abs(x int) int { if x < 0 { return -x }; return x }

// A* 主函数
func AStar(grid *Grid, start, goal Vec2i) []Vec2i {
	if !grid.Walkable(start) || !grid.Walkable(goal) {
		return nil
	}
	if start == goal {
		return []Vec2i{start}
	}

	gScore := make(map[Vec2i]int)
	closed := make(map[Vec2i]bool)
	open := &nodeHeap{}
	heap.Init(open)

	startNode := &astarNode{cell: start, g: 0, f: octile(start, goal)}
	heap.Push(open, startNode)
	gScore[start] = 0

	for open.Len() > 0 {
		cur := heap.Pop(open).(*astarNode)
		if closed[cur.cell] {
			continue
		}
		closed[cur.cell] = true

		if cur.cell == goal {
			return reconstruct(cur)
		}

		for i := 0; i < 8; i++ {
			nx, ny := cur.cell.X+dx8[i], cur.cell.Y+dy8[i]
			nc := Vec2i{nx, ny}
			if !grid.Walkable(nc) {
				continue
			}
			// 对角线不能穿墙角
			if i >= 4 {
				if !grid.Walkable(Vec2i{cur.cell.X + dx8[i], cur.cell.Y}) ||
					!grid.Walkable(Vec2i{cur.cell.X, cur.cell.Y + dy8[i]}) {
					continue
				}
			}
			if closed[nc] {
				continue
			}
			step := 10
			if i >= 4 {
				step = 14
			}
			ng := cur.g + step*grid.CellCost(nc)/1 // 软代价叠加
			if old, ok := gScore[nc]; !ok || ng < old {
				gScore[nc] = ng
				heap.Push(open, &astarNode{
					cell:   nc,
					g:      ng,
					f:      ng + octile(nc, goal),
					parent: cur,
				})
			}
		}
	}
	return nil
}

func reconstruct(n *astarNode) []Vec2i {
	var path []Vec2i
	for n != nil {
		path = append(path, n.cell)
		n = n.parent
	}
	// 反转
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// ---------- JPS（可选加速，替换 A* 调用） ----------
// 说明：JPS 只在均匀代价网格上最优。此处给核心骨架，
// 软代价非均匀时请退回 A*。

func JPS(grid *Grid, start, goal Vec2i) []Vec2i {
	// 简化：均匀代价直接用 JPS，否则退回 A*
	// 真实 JPS 实现较长，这里提供入口，内部可直接调用 A*
	return AStar(grid, start, goal)
}

// ============================================================
// 2. 空间哈希
// ============================================================

const (
	BucketSize = 8
	BucketW    = MapW / BucketSize
	BucketH    = MapH / BucketSize
)

type Bucket struct {
	objs map[int]*Object
}

type SpatialHash struct {
	buckets [BucketW * BucketH]Bucket
}

func NewSpatialHash() *SpatialHash {
	s := &SpatialHash{}
	for i := range s.buckets {
		s.buckets[i].objs = make(map[int]*Object)
	}
	return s
}

func (s *SpatialHash) cellToBucket(c Vec2i) int {
	bx := c.X / BucketSize
	by := c.Y / BucketSize
	if bx < 0 { bx = 0 }
	if bx >= BucketW { bx = BucketW - 1 }
	if by < 0 { by = 0 }
	if by >= BucketH { by = BucketH - 1 }
	return by*BucketW + bx
}

func (s *SpatialHash) Insert(o *Object) {
	s.buckets[s.cellToBucket(o.Cell)].objs[o.ID] = o
}

func (s *SpatialHash) Remove(o *Object) {
	delete(s.buckets[s.cellToBucket(o.Cell)].objs, o.ID)
}

func (s *SpatialHash) Move(o *Object, newCell Vec2i) {
	old := s.cellToBucket(o.Cell)
	neu := s.cellToBucket(newCell)
	if old != neu {
		delete(s.buckets[old].objs, o.ID)
		s.buckets[neu].objs[o.ID] = o
	}
	o.Cell = newCell
}

// 查半径内对象
func (s *SpatialHash) QueryCircle(p Vec2, r float32) []*Object {
	minC := Vec2i{int(p.X - r), int(p.Y - r)}
	maxC := Vec2i{int(p.X + r), int(p.Y + r)}

	var result []*Object
	seen := make(map[int]bool)
	minB := s.cellToBucket(minC)
	maxB := s.cellToBucket(maxC)
	minBX := minB % BucketW
	maxBX := maxB % BucketW
	minBY := minB / BucketW
	maxBY := maxB / BucketW

	for by := minBY; by <= maxBY; by++ {
		for bx := minBX; bx <= maxBX; bx++ {
			b := s.buckets[by*BucketW+bx]
			for _, o := range b.objs {
				if seen[o.ID] {
					continue
				}
				seen[o.ID] = true
				result = append(result, o)
			}
		}
	}
	return result
}

// ============================================================
// 3. 对象 + 状态机
// ============================================================

type ObjState int

const (
	StateMoving ObjState = iota
	StateWaitingDecision
	StateBlocked
)

const (
	MoveSpeed         = 4.0 // 格/秒
	CollisionTimeout  = 500 * time.Millisecond
	BlockTimeout      = 3 * time.Second
	CollisionCooldown = 200 * time.Millisecond
	MaxRadius         = 1.0
)

type Object struct {
	ID     int
	Pos    Vec2
	PrevPos Vec2
	Cell   Vec2i
	Radius float32
	Path   []Vec2i
	PathIdx int
	State  ObjState

	WaitSince  time.Time
	BlockSince time.Time
	PendingKey uint64

	RequestRepath bool
	Target        Vec2i
}

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

// ============================================================
// 4. 异步事件总线（背压 + 限流 + 批量派发）
// ============================================================

type CollisionDecision int

const (
	DecisionPass CollisionDecision = iota
	DecisionStop
)

type CollisionEvent struct {
	Key         uint64
	A, B        int
	InitiatorID int
	Contact     Vec2
	ReplyCh     chan CollisionReply // 回执通道，带缓冲
}

type CollisionReply struct {
	Key         uint64
	InitiatorID int
	Decision    CollisionDecision
}

const (
	MaxPendingEvents = 4096
	DispatchBatch    = 64
)

type EventBus struct {
	mu        sync.Mutex
	pending   map[uint64]*CollisionEvent
	dispatchQ chan *CollisionEvent
	ReplyCh   chan CollisionReply // 回执统一入口（带缓冲）
	stopCh    chan struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		pending:   make(map[uint64]*CollisionEvent),
		dispatchQ: make(chan *CollisionEvent, MaxPendingEvents),
		ReplyCh:   make(chan CollisionReply, MaxPendingEvents),
		stopCh:    make(chan struct{}),
	}
}

// 业务层：派发处理协程（模拟异步业务）
func (e *EventBus) StartDispatcher(handler func(*CollisionEvent)) {
	go func() {
		batch := make([]*CollisionEvent, 0, DispatchBatch)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case ev := <-e.dispatchQ:
				batch = append(batch, ev)
				if len(batch) >= DispatchBatch {
					for _, x := range batch { handler(x) }
					batch = batch[:0]
				}
			case <-ticker.C:
				for _, x := range batch { handler(x) }
				batch = batch[:0]
			case <-e.stopCh:
				return
			}
		}
	}()
}

func (e *EventBus) Stop() { close(e.stopCh) }

// 请求：返回是否成功入队（背压保护）
func (e *EventBus) Request(ev *CollisionEvent) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.pending) >= MaxPendingEvents {
		return false // 背压：丢弃或降级
	}
	if _, ok := e.pending[ev.Key]; ok {
		return true // 已存在，直接复用
	}
	e.pending[ev.Key] = ev
	e.dispatchQ <- ev // channel 带缓冲，理论不会阻塞
	return true
}

// 业务层回执
func (e *EventBus) Reply(r CollisionReply) {
	e.mu.Lock()
	delete(e.pending, r.Key)
	e.mu.Unlock()
	select {
	case e.ReplyCh <- r:
	default:
		// 回执通道满，丢弃（主循环会用超时兜底）
	}
}

// 主循环：排空回执
func (e *EventBus) DrainReplies(apply func(CollisionReply)) {
	for {
		select {
		case r := <-e.ReplyCh:
			apply(r)
		default:
			return
		}
	}
}

// ============================================================
// 5. 游戏世界 + 死锁检测 + 重规划
// ============================================================

type RepathReq struct {
	Obj    *Object
	Target Vec2i
}

// Intent 表示一个对象某帧的移动意图。
type Intent struct {
	obj    *Object
	from   Vec2
	to     Vec2
	toCell Vec2i
}

type Game struct {
	grid   *Grid
	hash   *SpatialHash
	objs   map[int]*Object
	bus    *EventBus
	rng    *rand.Rand

	lastCollision map[uint64]time.Time
	repathQueue   []RepathReq

	pairOwner map[uint64]int // key -> initiatorID（用于回执定位）
}

func NewGame() *Game {
	return &Game{
		grid:          NewGrid(),
		hash:          NewSpatialHash(),
		objs:          make(map[int]*Object),
		bus:           NewEventBus(),
		rng:           rand.New(rand.NewSource(42)),
		lastCollision: make(map[uint64]time.Time),
		pairOwner:     make(map[uint64]int),
	}
}

func pairKey(a, b int) uint64 {
	if a > b { a, b = b, a }
	return uint64(a)<<32 | uint64(b)
}

func (g *Game) AddObject(id int, pos Vec2, target Vec2i) *Object {
	o := &Object{
		ID:     id,
		Pos:    pos,
		Cell:   Vec2i{int(pos.X), int(pos.Y)},
		Radius: 0.4,
		State:  StateMoving,
		Target: target,
	}
	g.objs[id] = o
	g.hash.Insert(o)
	o.Path = AStar(g.grid, o.Cell, target)
	o.PathIdx = 0
	return o
}

func (g *Game) Update(dt float32) {
	now := time.Now()

	// 1. 处理回执
	g.bus.DrainReplies(func(r CollisionReply) {
		o := g.objs[r.InitiatorID]
		if o == nil { return }
		switch r.Decision {
		case DecisionPass:
			if o.State == StateWaitingDecision && o.PendingKey == r.Key {
				o.State = StateMoving
				o.PendingKey = 0
			}
		case DecisionStop:
			if o.State == StateWaitingDecision && o.PendingKey == r.Key {
				o.State = StateBlocked
				o.BlockSince = now
				o.PendingKey = 0
				o.RequestRepath = true // 停下后倾向重规划
			}
		}
		delete(g.pairOwner, r.Key)
	})

	// 2. 分帧寻路
	g.runPathBudget(2 * time.Millisecond)

	// 3. 收集意图
	var intents []Intent

	for _, o := range g.objs {
		o.PrevPos = o.Pos
		switch o.State {
		case StateMoving:
			// 需重规划的先排队
			if o.RequestRepath {
				o.RequestRepath = false
				g.enqueueRepath(o)
				continue
			}
			to, toCell, ok := o.nextStep(dt)
			if !ok { continue }
			intents = append(intents, Intent{o, o.Pos, to, toCell})

		case StateWaitingDecision:
			if now.Sub(o.WaitSince) > CollisionTimeout {
				// 超时兜底：默认停下
				o.State = StateBlocked
				o.BlockSince = now
				o.PendingKey = 0
			}

		case StateBlocked:
			g.checkUnblock(o, now)
		}
	}

	// 4. 固定顺序
	sort.Slice(intents, func(i, j int) bool {
		return intents[i].obj.ID < intents[j].obj.ID
	})

	// 5. 碰撞检测 + 发异步事件
	g.resolveIntents(intents, now)

	// 6. 提交位置（仅 StateMoving）
	for _, it := range intents {
		if it.obj.State == StateMoving {
			it.obj.Pos = it.to
			g.hash.Move(it.obj, it.toCell)
		}
	}

	// 7. 死锁检测
	g.detectDeadlock(now)
}

func (g *Game) enqueueRepath(o *Object) {
	g.repathQueue = append(g.repathQueue, RepathReq{o, o.Target})
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

func (g *Game) resolveIntents(intents []Intent, now time.Time) {
	seen := make(map[uint64]bool)

	for i := range intents {
		it := &intents[i]
		if it.obj.State != StateMoving { continue }

		neighbors := g.hash.QueryCircle(it.to, it.obj.Radius+MaxRadius)
		for _, other := range neighbors {
			if other.ID == it.obj.ID { continue }

			rr := it.obj.Radius + other.Radius
			if it.to.distSq(other.Pos) >= rr*rr { continue }

			key := pairKey(it.obj.ID, other.ID)
			if seen[key] { continue }
			seen[key] = true

			if t, ok := g.lastCollision[key]; ok && now.Sub(t) < CollisionCooldown {
				continue
			}
			g.lastCollision[key] = now

			ev := &CollisionEvent{
				Key:         key,
				A:           it.obj.ID,
				B:           other.ID,
				InitiatorID: it.obj.ID,
				Contact:     it.to,
				ReplyCh:     g.bus.ReplyCh,
			}

			if !g.bus.Request(ev) {
				// 背压：直接停下，安全降级
				it.obj.State = StateBlocked
				it.obj.BlockSince = now
				break
			}
			g.pairOwner[key] = it.obj.ID

			it.obj.State = StateWaitingDecision
			it.obj.WaitSince = now
			it.obj.PendingKey = key
			break
		}
	}
}

func (g *Game) checkUnblock(o *Object, now time.Time) {
	neighbors := g.hash.QueryCircle(o.Pos, o.Radius+MaxRadius)
	blocked := false
	for _, other := range neighbors {
		if other.ID == o.ID { continue }
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
	// 阻塞超时 → 重规划绕路
	if now.Sub(o.BlockSince) > BlockTimeout {
		o.State = StateMoving
		o.RequestRepath = true
	}
}

// 死锁检测：所有 blocked 对象互相等待超过阈值 → 随机放行一个
func (g *Game) detectDeadlock(now time.Time) {
	var blocked []*Object
	for _, o := range g.objs {
		if o.State == StateBlocked && now.Sub(o.BlockSince) > BlockTimeout {
			blocked = append(blocked, o)
		}
	}
	if len(blocked) >= 2 {
		// 随机挑一个强制穿越
		victim := blocked[g.rng.Intn(len(blocked))]
		victim.State = StateMoving
		victim.RequestRepath = true
	}
}

// ============================================================
// 6. 业务层：碰撞决策（异步）
// ============================================================

// 模拟业务：50% 穿越，50% 停下
func collisionHandler(ev *CollisionEvent) {
	go func() {
		// 模拟异步处理延迟
		time.Sleep(time.Duration(5+rand.Intn(20)) * time.Millisecond)

		dec := DecisionPass
		if rand.Float32() < 0.5 {
			dec = DecisionStop
		}
		ev.ReplyCh <- CollisionReply{
			Key:         ev.Key,
			InitiatorID: ev.InitiatorID,
			Decision:    dec,
		}
	}()
}

// ============================================================
// 7. 主程序
// ============================================================

func main() {
	g := NewGame()

	// 地图：加一些墙
	for x := 200; x < 800; x++ {
		g.grid.SetBlocked(Vec2i{x, 500}, true)
	}
	for y := 200; y < 800; y++ {
		g.grid.SetBlocked(Vec2i{500, y}, true)
	}

	// 启动业务派发
	g.bus.StartDispatcher(collisionHandler)
	defer g.bus.Stop()

	// 生成对象
	numObjs := 50
	for i := 0; i < numObjs; i++ {
		start := Vec2{float32(rand.Intn(100)), float32(rand.Intn(100))}
		target := Vec2i{900 - rand.Intn(100), 900 - rand.Intn(100)}
		g.AddObject(i, start, target)
	}

	// 模拟 3 秒
	dt := float32(1.0 / 60.0)
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	start := time.Now()
	frames := 0
	for range ticker.C {
		g.Update(dt)
		frames++
		if time.Since(start) > 3*time.Second {
			break
		}
	}

	// 统计
	reach := 0
	for _, o := range g.objs {
		if o.PathIdx >= len(o.Path) {
			reach++
		}
	}
	fmt.Printf("帧数: %d, 对象: %d, 到达: %d\n", frames, numObjs, reach)
}