package game

import "github.com/gulurence/cherry-server/examples/login-gate/worldsim"

// aoiState 实现灯塔（lighthouse）兴趣管理：把对象按所在塔索引，把玩家按订阅
// 的塔索引，用于视野内的进入/离开/移动事件扇出。
//
// 塔大小 50×50 格 -> 1000×1000 世界分成 20×20 个塔；玩家视野半径 50 格，
// 即 100×100 的视野，实际覆盖到的塔至多 3×3 个。
type aoiState struct {
	towerW    int
	towerH    int
	towerSize int
	viewRange int

	objTower     map[int]int
	objCell      map[int]worldsim.Vec2i
	towerObjs    map[int]map[int]struct{}   // 塔 -> 对象集合
	playerTowers map[int64]map[int]struct{} // uid -> 订阅塔集合
	towerPlayers map[int]map[int64]struct{} // 塔 -> 订阅 uid 集合（倒排）
}

func (a *aoiState) init() {
	a.towerSize = 50
	a.viewRange = 50
	a.towerW = worldsim.MapW / a.towerSize // 20
	a.towerH = worldsim.MapH / a.towerSize // 20

	a.objTower = make(map[int]int)
	a.objCell = make(map[int]worldsim.Vec2i)
	a.towerObjs = make(map[int]map[int]struct{})
	a.playerTowers = make(map[int64]map[int]struct{})
	a.towerPlayers = make(map[int]map[int64]struct{})
}

// towerKey 返回格子 (x,y) 所在塔的编号。
func (a *aoiState) towerKey(x, y int) int {
	tx := x / a.towerSize
	ty := y / a.towerSize
	if tx < 0 {
		tx = 0
	}
	if tx >= a.towerW {
		tx = a.towerW - 1
	}
	if ty < 0 {
		ty = 0
	}
	if ty >= a.towerH {
		ty = a.towerH - 1
	}
	return ty*a.towerW + tx
}

// towersInView 返回以 (cx,cy) 为中心、viewRange 为半径的矩形覆盖到的所有塔。
func (a *aoiState) towersInView(cx, cy int) []int {
	t0 := a.towerKey(cx-a.viewRange, cy-a.viewRange)
	t1 := a.towerKey(cx+a.viewRange, cy+a.viewRange)
	tx0, ty0 := t0%a.towerW, t0/a.towerW
	tx1, ty1 := t1%a.towerW, t1/a.towerW

	var out []int
	for ty := ty0; ty <= ty1; ty++ {
		for tx := tx0; tx <= tx1; tx++ {
			out = append(out, ty*a.towerW+tx)
		}
	}
	return out
}

func (a *aoiState) addObject(objID, cellX, cellY int) {
	t := a.towerKey(cellX, cellY)
	a.objTower[objID] = t
	a.objCell[objID] = worldsim.Vec2i{X: cellX, Y: cellY}
	if a.towerObjs[t] == nil {
		a.towerObjs[t] = make(map[int]struct{})
	}
	a.towerObjs[t][objID] = struct{}{}
}

func (a *aoiState) removeObject(objID int) {
	if t, ok := a.objTower[objID]; ok {
		if m := a.towerObjs[t]; m != nil {
			delete(m, objID)
		}
	}
	delete(a.objTower, objID)
	delete(a.objCell, objID)
}

// moveObject 更新对象所在塔与格子。
func (a *aoiState) moveObject(objID, newCellX, newCellY int) {
	old := a.objTower[objID]
	newT := a.towerKey(newCellX, newCellY)
	if old != newT {
		if m := a.towerObjs[old]; m != nil {
			delete(m, objID)
		}
		if a.towerObjs[newT] == nil {
			a.towerObjs[newT] = make(map[int]struct{})
		}
		a.towerObjs[newT][objID] = struct{}{}
		a.objTower[objID] = newT
	}
	a.objCell[objID] = worldsim.Vec2i{X: newCellX, Y: newCellY}
}

// subscribe 让 uid 订阅以 (cx,cy) 为中心的视野，返回订阅到的塔集合。
func (a *aoiState) subscribe(uid int64, cx, cy int) []int {
	towers := a.towersInView(cx, cy)
	set := make(map[int]struct{}, len(towers))
	for _, t := range towers {
		set[t] = struct{}{}
		if a.towerPlayers[t] == nil {
			a.towerPlayers[t] = make(map[int64]struct{})
		}
		a.towerPlayers[t][uid] = struct{}{}
	}
	a.playerTowers[uid] = set
	return towers
}

// resubscribe 更新 uid 的订阅，返回 (新增塔, 移除塔)。
func (a *aoiState) resubscribe(uid int64, cx, cy int) (added, removed []int) {
	old := a.playerTowers[uid]
	newTowers := a.towersInView(cx, cy)
	newSet := make(map[int]struct{}, len(newTowers))

	for _, t := range newTowers {
		newSet[t] = struct{}{}
		if _, ok := old[t]; !ok {
			added = append(added, t)
		}
		if a.towerPlayers[t] == nil {
			a.towerPlayers[t] = make(map[int64]struct{})
		}
		a.towerPlayers[t][uid] = struct{}{}
	}
	for t := range old {
		if _, ok := newSet[t]; !ok {
			removed = append(removed, t)
			if m := a.towerPlayers[t]; m != nil {
				delete(m, uid)
			}
		}
	}
	a.playerTowers[uid] = newSet
	return added, removed
}

func (a *aoiState) unsubscribe(uid int64) {
	for t := range a.playerTowers[uid] {
		if m := a.towerPlayers[t]; m != nil {
			delete(m, uid)
		}
	}
	delete(a.playerTowers, uid)
}
