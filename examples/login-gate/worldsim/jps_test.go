package worldsim

import (
	"math/rand"
	"testing"
)

// pathOctileCost 计算 8 方向路径的 Octile 代价（直步 10、斜步 14）。
func pathOctileCost(path []Vec2i) int {
	cost := 0
	for i := 1; i < len(path); i++ {
		dx := abs(path[i].X - path[i-1].X)
		dy := abs(path[i].Y - path[i-1].Y)
		if dx+dy == 1 {
			cost += 10
		} else {
			cost += 14
		}
	}
	return cost
}

// TestJPSMatchesAStar 在带障碍的小地图上穷举所有可通行 (start, goal) 对，
// 验证 JPS 与 A* 在「可达性、路径合法性、最优代价」上完全一致。
func TestJPSMatchesAStar(t *testing.T) {
	const region = 22 // 0..21 开放，其余阻挡
	grid := NewGrid()
	for y := 0; y < MapH; y++ {
		for x := 0; x < MapW; x++ {
			if x >= region || y >= region {
				grid.SetBlocked(Vec2i{x, y}, true)
			}
		}
	}
	// 放置各种形状的障碍，专门触发强制邻居 / 穿墙角检测 / 绕行。
	// 1) 水平墙（留两个口）：y=5, x=2..17 阻挡，留 x=8、x=14 两个口。
	for x := 2; x <= 17; x++ {
		if x == 8 || x == 14 {
			continue
		}
		grid.SetBlocked(Vec2i{x, 5}, true)
	}
	// 2) 垂直墙：x=15, y=9..19 阻挡。
	for y := 9; y <= 19; y++ {
		grid.SetBlocked(Vec2i{15, y}, true)
	}
	// 3) 孤立单格 + 拐角，制造穿墙角与强制邻居场景。
	grid.SetBlocked(Vec2i{3, 10}, true)
	grid.SetBlocked(Vec2i{4, 10}, true)
	grid.SetBlocked(Vec2i{3, 11}, true)
	grid.SetBlocked(Vec2i{11, 2}, true)
	grid.SetBlocked(Vec2i{11, 3}, true)

	pfA := NewPathfinder()
	pfJ := NewPathfinder()
	w := grid.walkable

	for sy := 0; sy < region; sy++ {
		for sx := 0; sx < region; sx++ {
			start := Vec2i{sx, sy}
			if !w[start.Y*MapW+start.X] {
				continue
			}
			for gy := 0; gy < region; gy++ {
				for gx := 0; gx < region; gx++ {
					goal := Vec2i{gx, gy}
					if !w[goal.Y*MapW+goal.X] {
						continue
					}
					a := pfA.FindWalkable(w, start, goal, 0)
					j := pfJ.FindJPS(w, start, goal, 0)
					if (a == nil) != (j == nil) {
						t.Fatalf("可达性不一致 start=%v goal=%v A*=%v JPS=%v", start, goal, a != nil, j != nil)
					}
					if a == nil {
						continue
					}
					if bad := ValidatePath(grid, j); bad != len(j) {
						t.Fatalf("JPS 路径非法 start=%v goal=%v 首个非法步=%d path=%v", start, goal, bad, j)
					}
					if ca, cj := pathOctileCost(a), pathOctileCost(j); ca != cj {
						t.Fatalf("代价不一致 start=%v goal=%v A*=%d JPS=%d", start, goal, ca, cj)
					}
				}
			}
		}
	}
}

// TestJPSMatchesAStarRandomMap 在真实随机地图上随机抽样若干对，验证 JPS 与 A* 一致。
func TestJPSMatchesAStarRandomMap(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	gm := GenerateMap(rng)
	grid := gm.Grid
	w := grid.walkable
	pfA := NewPathfinder()
	pfJ := NewPathfinder()

	for i := 0; i < 2000; i++ {
		sx, sy := rng.Intn(MapW), rng.Intn(MapH)
		gx, gy := rng.Intn(MapW), rng.Intn(MapH)
		start := Vec2i{sx, sy}
		goal := Vec2i{gx, gy}
		if !w[start.Y*MapW+start.X] || !w[goal.Y*MapW+goal.X] {
			continue
		}
		a := pfA.FindWalkable(w, start, goal, 0)
		j := pfJ.FindJPS(w, start, goal, 0)
		if (a == nil) != (j == nil) {
			t.Fatalf("可达性不一致 start=%v goal=%v A*=%v JPS=%v", start, goal, a != nil, j != nil)
		}
		if a == nil {
			continue
		}
		if bad := ValidatePath(grid, j); bad != len(j) {
			t.Fatalf("JPS 路径非法 start=%v goal=%v 首个非法步=%d", start, goal, bad)
		}
		if ca, cj := pathOctileCost(a), pathOctileCost(j); ca != cj {
			t.Fatalf("代价不一致 start=%v goal=%v A*=%d JPS=%d", start, goal, ca, cj)
		}
	}
}

// TestJPSShort 覆盖退化情形：起终点相同、相邻、越界、阻挡。
func TestJPSShort(t *testing.T) {
	grid := NewGrid()
	pf := NewPathfinder()
	w := grid.walkable

	if got := pf.FindJPS(w, Vec2i{5, 5}, Vec2i{5, 5}, 0); len(got) != 1 || got[0] != (Vec2i{5, 5}) {
		t.Fatalf("同点应返回单格路径，got=%v", got)
	}
	if got := pf.FindJPS(w, Vec2i{5, 5}, Vec2i{6, 5}, 0); len(got) != 2 {
		t.Fatalf("相邻直步路径应长 2，got=%v", got)
	}
	if got := pf.FindJPS(w, Vec2i{5, 5}, Vec2i{-1, 0}, 0); got != nil {
		t.Fatalf("越界起点应返回 nil，got=%v", got)
	}
	grid.SetBlocked(Vec2i{7, 5}, true)
	if got := pf.FindJPS(w, Vec2i{5, 5}, Vec2i{7, 5}, 0); got != nil {
		t.Fatalf("阻挡终点应返回 nil，got=%v", got)
	}
}
