package worldsim

import "math/rand"

// Terrain 格子地形类型（用于下发给客户端绘制）。
type Terrain byte

const (
	TerrainWalkable Terrain = 0 // 可通行
	TerrainMountain Terrain = 1 // 山
	TerrainRiver    Terrain = 2 // 河
)

// GridMap 同时携带寻路网格与下发给客户端的字符串地图。
type GridMap struct {
	Grid      *Grid
	MapString string
}

// GenerateMap 随机生成一张 1000×1000 地图：
//   - 山：若干随机簇（中心 + 抖动圆盘）
//   - 河：若干条带随机漂移的带状曲线
//
// 其余格子可通行。返回的 MapString 是 1,000,000 个字符的 '0'/'1'/'2' 串。
func GenerateMap(rng *rand.Rand) *GridMap {
	grid := NewGrid()
	terrain := make([]byte, MapW*MapH) // 默认 0 = 可通行

	// 山簇
	const mountainClusters = 70
	for i := 0; i < mountainClusters; i++ {
		cx := rng.Intn(MapW)
		cy := rng.Intn(MapH)
		radius := 6 + rng.Intn(15) // 6..20
		stampDisc(grid, terrain, cx, cy, radius, TerrainMountain, rng)
	}

	// 河流
	const riverCount = 10
	for i := 0; i < riverCount; i++ {
		genRiver(grid, terrain, rng)
	}

	return &GridMap{Grid: grid, MapString: string(terrain)}
}

// stampDisc 以 (cx,cy) 为中心、radius 为半径，把地形 stamp 进 grid 与 terrain，
// 边缘用 rng 抖动使轮廓不规则。
func stampDisc(grid *Grid, terrain []byte, cx, cy, radius int, t Terrain, rng *rand.Rand) {
	rr := radius * radius
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx := x - cx
			dy := y - cy
			// 距离加随机扰动，形成不规则边缘
			if dx*dx+dy*dy+rng.Intn(radius) <= rr {
				setTerrain(grid, terrain, x, y, t)
			}
		}
	}
}

// genRiver 生成一条随机漂移的河流（宽约 2~4 格）。
func genRiver(grid *Grid, terrain []byte, rng *rand.Rand) {
	// 从随机边界出发
	x, y := 0, 0
	switch rng.Intn(4) {
	case 0: // 上边界
		x, y = rng.Intn(MapW), 0
	case 1: // 下边界
		x, y = rng.Intn(MapW), MapH-1
	case 2: // 左边界
		x, y = 0, rng.Intn(MapH)
	case 3: // 右边界
		x, y = MapW-1, rng.Intn(MapH)
	}

	dx, dy := 1, 1
	if x == 0 {
		dx = 1
	}
	if x == MapW-1 {
		dx = -1
	}
	if y == 0 {
		dy = 1
	}
	if y == MapH-1 {
		dy = -1
	}

	steps := 200 + rng.Intn(400) // 200..599
	width := 2 + rng.Intn(2)     // 2..3
	for i := 0; i < steps; i++ {
		// 随机漂移方向
		if rng.Intn(6) == 0 {
			dx = rng.Intn(3) - 1
			dy = rng.Intn(3) - 1
			if dx == 0 && dy == 0 {
				dx = 1
			}
		}

		stampDisc(grid, terrain, x, y, width, TerrainRiver, rng)

		x += dx
		y += dy
		// 出界则反弹回边界内
		if x < 0 {
			x, dx = 0, 1
		}
		if x >= MapW {
			x, dx = MapW-1, -1
		}
		if y < 0 {
			y, dy = 0, 1
		}
		if y >= MapH {
			y, dy = MapH-1, -1
		}
	}
}

// setTerrain 设置单个格子的地形（阻挡且写 terrain 字符）。
func setTerrain(grid *Grid, terrain []byte, x, y int, t Terrain) {
	c := Vec2i{x, y}
	if !grid.InBounds(c) {
		return
	}
	grid.SetBlocked(c, true)
	terrain[c.Y*MapW+c.X] = byte('0') + byte(t)
}
