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
