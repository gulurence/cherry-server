package worldsim

import (
	"math/rand"
	"testing"
)

// TestSetClientPath 验证客户端预寻路：合法路径被直接采用，篡改路径回退服务器寻路。
func TestSetClientPath(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	gm := GenerateMap(rng)
	g := NewGame(gm.Grid, rng, 1)

	start, ok1 := nearestWalkable(gm.Grid, 500, 500)
	goal, ok2 := nearestWalkable(gm.Grid, 800, 800)
	if !ok1 || !ok2 {
		t.Fatal("找不到测试用可通行格")
	}

	o := &Object{ID: 1, Size: 1, Radius: RadiusForSize(1), Pos: Vec2{float32(start.X) + 0.5, float32(start.Y) + 0.5}}
	g.AddObject(o)

	serverPath := g.SetPath(o, goal)
	if serverPath == nil {
		t.Skip("随机地图下起终点不可达，跳过")
	}

	// 1. 合法客户端路径（= 服务器标准路径）应被直接采用。
	got := g.SetClientPath(o, goal, serverPath)
	if len(got) != len(serverPath) {
		t.Fatalf("合法客户端路径未被采用：got=%d want=%d", len(got), len(serverPath))
	}

	// 2. 篡改路径（瞬移）应回退到服务器寻路，且回退结果合法。
	tampered := []Vec2i{serverPath[0], goal}
	got2 := g.SetClientPath(o, goal, tampered)
	if got2 == nil {
		t.Fatal("篡改路径回退失败（返回 nil）")
	}
	if bad := ValidatePath(gm.Grid, got2); bad != len(got2) {
		t.Fatalf("回退结果非法，首个非法步=%d", bad)
	}

	// 3. 起点偏离 1 格（对象在客户端算路后又移动了）应被 rebase 对齐。
	offset := []Vec2i{serverPath[1]} // 去掉首格，模拟从第 1 格开始
	offset = append(offset, serverPath[2:]...)
	got3 := g.SetClientPath(o, goal, offset)
	if got3 == nil || len(got3) == 0 {
		t.Fatal("起点偏移 1 格时 rebase 失败")
	}
}
