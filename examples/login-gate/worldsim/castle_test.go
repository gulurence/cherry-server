package worldsim

import (
	"math/rand"
	"testing"
	"time"
)

// TestSetClientPathCastle 验证客户端路径穿过动态城堡时，服务端用 A* 绕行修正，
// 而不是整体回退或直接采纳穿墙路径。
func TestSetClientPathCastle(t *testing.T) {
	grid := NewGrid() // 全图可通行，便于精确控制城堡位置
	g := NewGame(grid, rand.New(rand.NewSource(1)), 1)

	target := Vec2i{X: 20, Y: 10}
	o := &Object{ID: 1, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)

	// 客户端不知道城堡，其路径是 (10,10)..(20,10) 的直线，正好穿过城堡。
	clientPath := make([]Vec2i, 0, 11)
	for x := 10; x <= 20; x++ {
		clientPath = append(clientPath, Vec2i{X: x, Y: 10})
	}

	// 放置一座 3×3 城堡，中心 (15,10)，挡住 x=14..16, y=9..11。
	for y := 9; y <= 11; y++ {
		for x := 14; x <= 16; x++ {
			grid.SetBlocked(Vec2i{X: x, Y: y}, true)
		}
	}

	got := g.SetClientPath(o, target, clientPath)
	if got == nil {
		t.Fatal("城堡绕行修正失败（返回 nil）")
	}
	if got[len(got)-1] != target {
		t.Fatalf("修正后路径终点=%v，应为 %v", got[len(got)-1], target)
	}
	if bad := ValidatePath(grid, got); bad != len(got) {
		t.Fatalf("修正后路径非法，首个非法步=%d", bad)
	}
	for _, c := range got {
		if !grid.Walkable(c) {
			t.Fatalf("修正后路径穿过城堡格 %v", c)
		}
	}
}

// TestAddCastleNotOnObject 验证城堡绝不压到对象所在格（含其 3×3 邻域）。
func TestAddCastleNotOnObject(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(2)), 1)

	o := &Object{ID: 1, Size: 1, Radius: 0.5, Pos: Vec2{100.5, 100.5}}
	g.AddObject(o)

	for i := 0; i < 200; i++ {
		c := g.AddCastle()
		if c == nil {
			continue
		}
		for _, cell := range c.Cells {
			if g.cellOccupied(cell) {
				t.Fatalf("城堡压到对象邻域：cell=%v", cell)
			}
		}
	}
}

// TestCastleCoverage 验证 SeedCastles 让城堡覆盖全图约 1/8（不重叠），并记录实际
// 放置数量为 castleTarget。
func TestCastleCoverage(t *testing.T) {
	grid := NewGrid() // 全图可通行，便于逼近理论上限
	g := NewGame(grid, rand.New(rand.NewSource(3)), 1)

	g.SeedCastles()

	got := g.AllCastleCells()
	want := castleTargetCount * castleSize * castleSize
	// 槽位互不重叠、全图可通行时，SeedCastles 应精确放置 castleTargetCount 座。
	if len(got) != want {
		t.Fatalf("城堡覆盖格子数=%d，应为 %d", len(got), want)
	}
	if g.castleTarget != len(g.castles) {
		t.Fatalf("castleTarget=%d 但实际城堡数=%d", g.castleTarget, len(g.castles))
	}
	seen := make(map[Vec2i]bool, len(got))
	for _, c := range got {
		if seen[c] {
			t.Fatalf("城堡格重叠：%v", c)
		}
		seen[c] = true
	}
}

// drainRepaths 轮询 worker 池并回填异步重寻路结果，直到 g.repaths 清空或超时。
func drainRepaths(t *testing.T, g *Game) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(g.repaths) > 0 && time.Now().Before(deadline) {
		g.pool.Drain(func(r PathResult) { g.ApplyRepathResult(r) })
		time.Sleep(time.Millisecond)
	}
	if len(g.repaths) > 0 {
		t.Fatal("异步重寻路结果超时")
	}
}

// TestFirstBlockedAhead 验证 firstBlockedAhead 只报告“前方 repairPathRange 格内”的阻挡：
// 紧邻下一格、范围内的中间格返回其偏移，超出范围的远端阻挡返回 -1。
func TestFirstBlockedAhead(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(10)), 1)

	o := &Object{ID: 1, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 190)
	for x := 11; x <= 200; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 200, Y: 10}

	grid.SetBlocked(Vec2i{X: 11, Y: 10}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != 0 {
		t.Fatalf("紧邻下一格被挡偏移应=0，实际=%d", d)
	}
	grid.SetBlocked(Vec2i{X: 11, Y: 10}, false)

	grid.SetBlocked(Vec2i{X: 40, Y: 10}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != 29 {
		t.Fatalf("范围内阻挡偏移应=29，实际=%d", d)
	}
	grid.SetBlocked(Vec2i{X: 40, Y: 10}, false)

	grid.SetBlocked(Vec2i{X: 190, Y: 10}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != -1 {
		t.Fatalf("远端阻挡应返回 -1，实际=%d", d)
	}
}

// TestFirstBlockedAheadCorner 验证斜向步因新城堡落在其正交邻居上而“穿墙角”时，
// firstBlockedAhead 也能检测到——即使城堡不在路径本身上（拐角修正）。
func TestFirstBlockedAheadCorner(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(16)), 1)

	o := &Object{ID: 6, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	// 斜向路径 (11,11),(12,12),...。第一/三步的正交邻居 (11,10)/(13,12) 都不在路径上。
	path := make([]Vec2i, 0, 10)
	for k := 1; k <= 10; k++ {
		path = append(path, Vec2i{X: 10 + k, Y: 10 + k})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 20, Y: 20}

	// 无城堡时全部合法。
	if d := g.firstBlockedAhead(o, repairPathRange); d != -1 {
		t.Fatalf("无城堡时不应判非法，实际偏移=%d", d)
	}

	// 压住第一步 (10,10)->(11,11) 的正交邻居 (11,10)：当前斜向步穿墙角，偏移 0。
	grid.SetBlocked(Vec2i{X: 11, Y: 10}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != 0 {
		t.Fatalf("第一步穿墙角偏移应=0，实际=%d", d)
	}
	grid.SetBlocked(Vec2i{X: 11, Y: 10}, false)

	// 压住第三步 (12,12)->(13,13) 的正交邻居 (13,12)：偏移 2。
	grid.SetBlocked(Vec2i{X: 13, Y: 12}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != 2 {
		t.Fatalf("第三步穿墙角偏移应=2，实际=%d", d)
	}
	grid.SetBlocked(Vec2i{X: 13, Y: 12}, false)

	// 正交邻居 (12,13) 同属第三步，但它是目标方向另一侧，压它同样穿墙角。
	grid.SetBlocked(Vec2i{X: 12, Y: 13}, true)
	if d := g.firstBlockedAhead(o, repairPathRange); d != 2 {
		t.Fatalf("第三步另一侧穿墙角偏移应=2，实际=%d", d)
	}
}

// TestUpdateRepathPlayer 验证城堡新增后，路径在 repairPathRange 内穿过新城堡的玩家，
// 在 Update 里走 worker 池异步重寻路绕行（而非走到城堡跟前才发现）。
func TestUpdateRepathPlayer(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(11)), 1)

	o := &Object{ID: 1, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 50)
	for x := 11; x <= 60; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 60, Y: 10}

	grid.SetBlocked(Vec2i{X: 40, Y: 10}, true)
	g.refreshSnapshot() // worker 读快照，须包含新城堡

	g.Update(0.05)

	if len(g.repaths) != 1 {
		t.Fatalf("范围内城堡应触发玩家异步重寻路，实际 repaths=%d", len(g.repaths))
	}
	if !o.RepathPending {
		t.Fatal("提交异步重寻路后 RepathPending 应为 true")
	}

	drainRepaths(t, g)

	if o.RepathPending {
		t.Fatal("重寻路回填后 RepathPending 应为 false")
	}
	if o.Path == nil {
		t.Fatal("重寻路失败：路径为空")
	}
	for _, c := range o.Path {
		if !grid.Walkable(c) {
			t.Fatalf("重寻路后路径仍穿过城堡格 %v", c)
		}
	}
	if o.Path[len(o.Path)-1] != (Vec2i{X: 60, Y: 10}) {
		t.Fatalf("重寻路后终点=%v，应为 (60,10)", o.Path[len(o.Path)-1])
	}
}

// TestUpdateRepathNPC 验证城堡新增后，路径在范围内穿过新城堡的 NPC 进入分帧重寻路队列，
// 并在 runPathBudget 中被绕行修正。
func TestUpdateRepathNPC(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(12)), 1)

	o := &Object{ID: 2, IsPlayer: false, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 50)
	for x := 11; x <= 60; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 60, Y: 10}

	grid.SetBlocked(Vec2i{X: 40, Y: 10}, true)

	g.Update(0.05)

	if len(g.repathQueue) != 1 {
		t.Fatalf("范围内城堡应进入 NPC 分帧重寻路队列，实际 repathQueue=%d", len(g.repathQueue))
	}
	if !o.RepathPending {
		t.Fatal("入队后 RepathPending 应为 true")
	}

	g.runPathBudget(100 * time.Millisecond)

	if o.RepathPending {
		t.Fatal("分帧重寻路处理后 RepathPending 应为 false")
	}
	if o.Path == nil {
		t.Fatal("重寻路失败：路径为空")
	}
	for _, c := range o.Path {
		if !grid.Walkable(c) {
			t.Fatalf("重寻路后路径仍穿过城堡格 %v", c)
		}
	}
	if o.Path[len(o.Path)-1] != (Vec2i{X: 60, Y: 10}) {
		t.Fatalf("重寻路后终点=%v，应为 (60,10)", o.Path[len(o.Path)-1])
	}
}

// TestUpdateRepathTarget 验证城堡新增压在对象目标上时，Update 直接清空路径停下，
// 不做无意义的重寻路。
func TestUpdateRepathTarget(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(13)), 1)

	o := &Object{ID: 3, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 10)
	for x := 11; x <= 20; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 20, Y: 10}

	grid.SetBlocked(Vec2i{X: 20, Y: 10}, true)

	g.Update(0.05)

	if o.Path != nil || o.PathIdx != 0 {
		t.Fatalf("目标被压应清空路径停下，实际 Path=%v PathIdx=%d", o.Path, o.PathIdx)
	}
	if len(g.repaths) != 0 {
		t.Fatalf("目标被压不应入队重寻路，实际 repaths=%d", len(g.repaths))
	}
}

// TestUpdateRepathFar 验证距离还很远（超过 repairPathRange）的城堡不触发重寻路：
// 对象保持原路径继续走，走近到范围内时再被 Update 触发。
func TestUpdateRepathFar(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(14)), 1)

	o := &Object{ID: 4, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 190)
	for x := 11; x <= 200; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 200, Y: 10}

	grid.SetBlocked(Vec2i{X: 190, Y: 10}, true)

	g.Update(0.05)

	if len(g.repaths) != 0 {
		t.Fatalf("远端城堡不应触发异步重寻路，实际 repaths=%d", len(g.repaths))
	}
	if len(g.repathQueue) != 0 {
		t.Fatalf("远端城堡不应进入分帧重寻路队列，实际 repathQueue=%d", len(g.repathQueue))
	}
	if o.RepathPending {
		t.Fatal("远端城堡不应置位 RepathPending")
	}
	if len(o.Path) != 190 || o.Path[0] != (Vec2i{X: 11, Y: 10}) {
		t.Fatalf("远端城堡应保持原路径，实际 pathLen=%d", len(o.Path))
	}
}

// TestUpdateRepathNextCell 验证紧邻下一格被挡时，本帧不前进（避免走进城堡），并触发重寻路。
func TestUpdateRepathNextCell(t *testing.T) {
	grid := NewGrid()
	g := NewGame(grid, rand.New(rand.NewSource(15)), 1)

	o := &Object{ID: 5, IsPlayer: true, Size: 1, Radius: 0.5, Pos: Vec2{10.5, 10.5}}
	g.AddObject(o)
	path := make([]Vec2i, 0, 10)
	for x := 11; x <= 20; x++ {
		path = append(path, Vec2i{X: x, Y: 10})
	}
	o.Path = path
	o.PathIdx = 0
	o.Target = Vec2i{X: 20, Y: 10}

	grid.SetBlocked(Vec2i{X: 11, Y: 10}, true)
	g.refreshSnapshot()

	before := o.Pos
	g.Update(0.05)

	if o.Pos != before {
		t.Fatalf("下一格被挡时本帧不应前进，Pos=%v → %v", before, o.Pos)
	}
	if len(g.repaths) != 1 {
		t.Fatalf("下一格被挡应触发异步重寻路，实际 repaths=%d", len(g.repaths))
	}
}
