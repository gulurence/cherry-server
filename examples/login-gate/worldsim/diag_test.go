package worldsim

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// nearestWalkable 在 (x,y) 附近按正方形扩展查找最近的可通行格子。
func nearestWalkable(grid *Grid, x, y int) (Vec2i, bool) {
	for r := 0; r < 64; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				c := Vec2i{x + dx, y + dy}
				if grid.Walkable(c) {
					return c, true
				}
			}
		}
	}
	return Vec2i{}, false
}

// pathCost 计算一条路径的总代价（直 10 / 斜 14），用于校验新旧实现结果一致。
func pathCost(p []Vec2i) int {
	if len(p) < 2 {
		return 0
	}
	cost := 0
	for i := 1; i < len(p); i++ {
		dx := abs(p[i].X - p[i-1].X)
		dy := abs(p[i].Y - p[i-1].Y)
		if dx != 0 && dy != 0 {
			cost += 14
		} else {
			cost += 10
		}
	}
	return cost
}

// TestPathfindOptimization 对比旧（map 版 AStar）与新（平铺数组版 Pathfinder）寻路耗时，
// 并校验两者结果一致（可达性一致、最短路径代价一致）。
func TestPathfindOptimization(t *testing.T) {
	pf := NewPathfinder()

	type scenario struct {
		name  string
		grid  *Grid
		start Vec2i
		goal  Vec2i
	}

	var scenarios []scenario
	// 全可通行：对角线一定可达。
	scenarios = append(scenarios, scenario{"全可通行", NewGrid(), Vec2i{0, 0}, Vec2i{MapW - 1, MapH - 1}})

	// 真实随机地图：覆盖可达 / 不可达。
	for _, seed := range []int64{1, 42, 2026} {
		gm := GenerateMap(rand.New(rand.NewSource(seed)))
		s, ok1 := nearestWalkable(gm.Grid, 0, 0)
		g, ok2 := nearestWalkable(gm.Grid, MapW-1, MapH-1)
		if !ok1 || !ok2 {
			continue
		}
		scenarios = append(scenarios, scenario{
			name:  fmt.Sprintf("seed=%d", seed),
			grid:  gm.Grid,
			start: s,
			goal:  g,
		})
	}

	for _, sc := range scenarios {
		// 旧实现（map 版 AStar）
		begin := time.Now()
		old := AStar(sc.grid, sc.start, sc.goal)
		oldCost := time.Since(begin)

		// 新实现（平铺数组版 Pathfinder，不设上限）
		begin = time.Now()
		newP := pf.Find(sc.grid, sc.start, sc.goal, 0)
		newCost := time.Since(begin)

		// 正确性：可达性一致，且（可达时）最短路径代价一致。
		if (old == nil) != (newP == nil) {
			t.Errorf("[%s] 可达性不一致: old=%v new=%v", sc.name, old != nil, newP != nil)
		}
		if old != nil && newP != nil && pathCost(old) != pathCost(newP) {
			t.Errorf("[%s] 路径代价不一致: old=%d new=%d", sc.name, pathCost(old), pathCost(newP))
		}

		speedup := float64(oldCost) / float64(newCost)
		t.Logf("[%s] (%d,%d)->(%d,%d) 旧=%v 新=%v 可达=%v 路点=%d 加速=%.1fx",
			sc.name, sc.start.X, sc.start.Y, sc.goal.X, sc.goal.Y,
			oldCost, newCost, newP != nil, len(newP), speedup)
	}
}
