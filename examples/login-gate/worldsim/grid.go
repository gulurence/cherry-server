// Package worldsim 提供 login-gate 示例的 1000×1000 网格世界模拟：
// 静态网格 + A* 寻路、空间哈希、对象状态机、阵营碰撞与随机地图生成。
//
// 该包是纯逻辑包，不依赖 cherry 框架，由 game 节点的 world actor 在自身
// goroutine 内串行驱动（见 nodes/game/actor_world.go 的 tick）。
package worldsim

import (
	"container/heap"
	"math"
)

// Vec2 连续坐标（格子内的浮点位置，用于平滑移动）。
type Vec2 struct{ X, Y float32 }

// Vec2i 离散格子坐标（整数）。
type Vec2i struct{ X, Y int }

func (a Vec2) distSq(b Vec2) float32 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

const (
	// MapW / MapH 世界尺寸（格子数）。
	MapW, MapH = 1000, 1000
)

// Grid 静态可通行网格。walkable 为 1D 数组，true 表示可通行。
type Grid struct {
	walkable []bool
	cost     []int
}

// NewGrid 创建一张全部可通行的空地图。
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

// InBounds 判断格子是否在世界范围内。
func (g *Grid) InBounds(c Vec2i) bool {
	return c.X >= 0 && c.X < MapW && c.Y >= 0 && c.Y < MapH
}

// Walkable 判断格子是否可通行（越界视为不可通行）。
func (g *Grid) Walkable(c Vec2i) bool {
	return g.InBounds(c) && g.walkable[g.idx(c)]
}

// SetBlocked 设置格子阻挡状态（blocked=true 表示不可通行）。
func (g *Grid) SetBlocked(c Vec2i, blocked bool) {
	if g.InBounds(c) {
		g.walkable[g.idx(c)] = !blocked
	}
}

// CellCost 返回格子的软代价（1 + 附加代价）。
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
	index  int
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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// AStar 在 grid 上求 start 到 goal 的 8 方向最短路径（Octile 启发）。
// 不可达或起终点不可通行时返回 nil。
//
// 这是基于 map 的遗留实现，仅作参考/对比与 JPS 兜底；生产寻路统一走
// Pathfinder.Find（平铺数组 + 零分配堆，见下）。
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
			ng := cur.g + step*grid.CellCost(nc)
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

// Pathfinder 复用平铺数组（生成戳标记）执行 A*，全程不分配 map、不为每个展开
// 节点分配堆内存，也不走 container/heap 的接口分派——用两个平行 int32 切片手写
// 二叉最小堆作开放集，避免 GC 抖动。供 NPC 游走与玩家 moveTo 等高频寻路使用，
// 单线程串行调用（world actor 驱动）。
type Pathfinder struct {
	gScore []int32
	gGen   []int32
	closed []int32
	parent []int32
	gen    int32

	// 开放集最小堆：openF[i] 与 openCell[i] 一一对应，按 f 排序。
	openF    []int32
	openCell []int32
}

func NewPathfinder() *Pathfinder {
	return &Pathfinder{
		gScore:   make([]int32, MapW*MapH),
		gGen:     make([]int32, MapW*MapH),
		closed:   make([]int32, MapW*MapH),
		parent:   make([]int32, MapW*MapH),
		openF:    make([]int32, 0, 1<<16),
		openCell: make([]int32, 0, 1<<16),
	}
}

// heapPush / heapPop 手写二叉最小堆（按 f 排序），复用切片容量，无接口分派。
func (pf *Pathfinder) heapPush(f, cell int32) {
	pf.openF = append(pf.openF, f)
	pf.openCell = append(pf.openCell, cell)
	i := len(pf.openF) - 1
	for i > 0 {
		p := (i - 1) / 2
		if pf.openF[p] <= f {
			break
		}
		pf.openF[i] = pf.openF[p]
		pf.openCell[i] = pf.openCell[p]
		i = p
	}
	pf.openF[i] = f
	pf.openCell[i] = cell
}

func (pf *Pathfinder) heapPop() int32 {
	cell := pf.openCell[0]
	n := len(pf.openF) - 1
	if n > 0 {
		f := pf.openF[n]
		c := pf.openCell[n]
		pf.openF = pf.openF[:n]
		pf.openCell = pf.openCell[:n]
		i := 0
		for {
			l := 2*i + 1
			if l >= n {
				break
			}
			r := l + 1
			m := l
			if r < n && pf.openF[r] < pf.openF[l] {
				m = r
			}
			if pf.openF[m] >= f {
				break
			}
			pf.openF[i] = pf.openF[m]
			pf.openCell[i] = pf.openCell[m]
			i = m
		}
		pf.openF[i] = f
		pf.openCell[i] = c
	} else {
		pf.openF = pf.openF[:0]
		pf.openCell = pf.openCell[:0]
	}
	return cell
}

// octileH 返回 (x,y) 到 (gx,gy) 的 Octile 启发值，内联避免构造 Vec2i。
func octileH(x, y, gx, gy int) int32 {
	dx := x - gx
	if dx < 0 {
		dx = -dx
	}
	dy := y - gy
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return int32(10*(dx-dy) + 14*dy)
	}
	return int32(10*(dy-dx) + 14*dx)
}

// Find 在 grid 上求 start 到 goal 的 8 方向路径。maxExpand>0 时为展开节点上限，
// 超过则返回 nil（视为不可达）；maxExpand<=0 表示不设上限（玩家 moveTo 全图寻路）。
// 单线程串行调用（world actor 驱动），内部委托 FindWalkable 读取 grid.walkable。
func (pf *Pathfinder) Find(grid *Grid, start, goal Vec2i, maxExpand int) []Vec2i {
	if !grid.Walkable(start) || !grid.Walkable(goal) {
		return nil
	}
	return pf.FindWalkable(grid.walkable, start, goal, maxExpand)
}

// FindWalkable 在给定的 walkable 快照（平铺 []bool，长度 MapW*MapH）上求 start 到 goal
// 的 8 方向路径。与 Find 同语义，但不依赖 *Grid：并发 worker 各自持独立 Pathfinder，
// 读取同一份只读快照（城堡增删后由上层整体换新的快照），因此无需加锁。
func (pf *Pathfinder) FindWalkable(walkable []bool, start, goal Vec2i, maxExpand int) []Vec2i {
	if start.X < 0 || start.Y < 0 || start.X >= MapW || start.Y >= MapH ||
		!walkable[start.Y*MapW+start.X] {
		return nil
	}
	if goal.X < 0 || goal.Y < 0 || goal.X >= MapW || goal.Y >= MapH ||
		!walkable[goal.Y*MapW+goal.X] {
		return nil
	}
	if start == goal {
		return []Vec2i{start}
	}

	pf.gen++
	gen := pf.gen
	pf.openF = pf.openF[:0]
	pf.openCell = pf.openCell[:0]

	si := start.Y*MapW + start.X
	gi := goal.Y*MapW + goal.X
	gx, gy := goal.X, goal.Y
	pf.gScore[si] = 0
	pf.gGen[si] = gen
	pf.heapPush(octileH(start.X, start.Y, gx, gy), int32(si))

	expanded := 0
	for len(pf.openF) > 0 {
		ci := int(pf.heapPop())
		if pf.closed[ci] == gen {
			continue // 过期条目：该格子已以更优 g 出队
		}
		pf.closed[ci] = gen
		expanded++
		if maxExpand > 0 && expanded > maxExpand {
			return nil
		}

		if ci == gi {
			return pf.reconstructPath(si, gi)
		}

		cx, cy := ci%MapW, ci/MapW
		baseG := pf.gScore[ci]
		for i := 0; i < 8; i++ {
			nx, ny := cx+dx8[i], cy+dy8[i]
			if nx < 0 || nx >= MapW || ny < 0 || ny >= MapH {
				continue
			}
			ni := ny*MapW + nx
			if !walkable[ni] {
				continue
			}
			// 对角线不能穿墙角
			if i >= 4 && (!walkable[cy*MapW+nx] || !walkable[ny*MapW+cx]) {
				continue
			}
			if pf.closed[ni] == gen {
				continue
			}
			step := int32(10)
			if i >= 4 {
				step = 14
			}
			// 格子软代价恒为 1（Grid.cost 从未被写入），故直接乘 step。
			ng := baseG + step
			if pf.gGen[ni] != gen || ng < pf.gScore[ni] {
				pf.gGen[ni] = gen
				pf.gScore[ni] = ng
				pf.parent[ni] = int32(ci)
				pf.heapPush(ng+octileH(nx, ny, gx, gy), int32(ni))
			}
		}
	}
	return nil
}

// reconstructPath 沿 parent 链从 goal 回溯到 start，再反转为正序路径。
func (pf *Pathfinder) reconstructPath(startIdx, goalIdx int) []Vec2i {
	n := 1
	for i := goalIdx; i != startIdx; i = int(pf.parent[i]) {
		n++
	}
	path := make([]Vec2i, 0, n)
	for i := goalIdx; ; i = int(pf.parent[i]) {
		path = append(path, Vec2i{X: i % MapW, Y: i / MapW})
		if i == startIdx {
			break
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func reconstruct(n *astarNode) []Vec2i {
	var path []Vec2i
	for n != nil {
		path = append(path, n.cell)
		n = n.parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// JPS 是 A* 的入口替身：非均匀代价网格下直接退回 A*。
func JPS(grid *Grid, start, goal Vec2i) []Vec2i {
	return AStar(grid, start, goal)
}

// ValidatePath 校验 path 是否为 grid 上合法的 8 方向路径：
//   - 每格可通行；
//   - 相邻两格为 8 方向相邻（|dx|<=1, |dy|<=1，非原地）；
//   - 斜向步不穿墙角。
//
// 返回第一个非法步的下标；等于 len(path) 表示全部合法（空路径返回 0）。
func ValidatePath(grid *Grid, path []Vec2i) int {
	if len(path) == 0 {
		return 0
	}
	if !grid.Walkable(path[0]) {
		return 0
	}
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 || (dx == 0 && dy == 0) {
			return i
		}
		if !grid.Walkable(b) {
			return i
		}
		if dx != 0 && dy != 0 {
			if !grid.Walkable(Vec2i{a.X + dx, a.Y}) || !grid.Walkable(Vec2i{a.X, a.Y + dy}) {
				return i
			}
		}
	}
	return len(path)
}

// RebasePath 把路径对齐到对象当前格 cur：找到路径中与 cur 相同或 8 相邻的
// 格子，丢弃其之前的路点（相邻时把 cur 插到最前，容忍对象在算路后又移动了
// 1 格）。偏离太远（无 8 相邻格子）返回 nil，表示应重新寻路。
func RebasePath(cur Vec2i, path []Vec2i) []Vec2i {
	for i, c := range path {
		dx := abs(c.X - cur.X)
		dy := abs(c.Y - cur.Y)
		if dx > 1 || dy > 1 {
			continue
		}
		if dx == 0 && dy == 0 {
			return path[i:]
		}
		out := make([]Vec2i, 0, len(path)-i+1)
		out = append(out, cur)
		out = append(out, path[i:]...)
		return out
	}
	return nil
}
