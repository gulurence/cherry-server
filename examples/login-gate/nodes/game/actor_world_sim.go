// 本文件是 game 节点上的世界模拟 actor（actorID = "worldsim"），承载完整的
// 实时网格世界：随机地图、600 个 NPC 游走、阵营碰撞、灯塔（AOI）视野、周期
// tick，以及面向客户端的全部推送。协议层 ActorWorld 通过 Call/CallWait 调用本
// 文件注册的远程函数，两者各自运行在自己的 goroutine 上，所有模拟逻辑串行跑在
// 本 actor 的 goroutine 内（p.Timer().Add 驱动，无并发）。
//
// 代码框架（自上而下）：
//
//   1. 包级常量：tickInterval(50ms) / npcPerFaction(200) / playerIdleWait(60s)
//   2. RPC 参数类型：simEnterArg / simMoveToArg / simViewArg / simLeaveArg
//      （仅同一 game 节点内以 Go 对象传递，不经过线上序列化）
//   3. playerEntry：记录已进入世界的玩家（uid / agentPath / 阵营 / 对象 id / 视野中心 / 最后活跃）
//   4. ActorWorldSim 结构体：sim(纯逻辑世界) / mapStr(下发地图) / rng / nextObjID /
//      players / objOwner / aoi(灯塔) / lastTick / collStats(碰撞统计)
//   5. 生命周期：AliasID() -> OnInit()（生成地图、注册远程函数、注入回调、启动 tick）
//   6. 世界初始化：spawnNPCs()（红/黑/绿各 200 个，随机尺寸 1~3）、randomWalkable()
//   7. 远程函数（被 ActorWorld 调用）：enter / moveTo / view / leave
//   8. 每帧模拟：tick() -> UpdateCastles(动态城堡增删) + sim.Update + WanderNPCs + syncAOI
//      + reportCollisions + sweepIdle；syncAOI()（对象换格/换塔扇出事件）、
//      sweepIdle()（清理超时玩家）、removePlayer()
//  11a. 动态城堡：castleMapString（进入快照） / castleCellsMsg / broadcastCastleCells（增删按灯塔扇出）
//      / castleCellsByTower（视野切换时补发塔内城堡快照）
//   9. 碰撞回调：onCollision()（统计阵营关系）、reportCollisions()（每秒汇总一次日志）
//  10. 寻路回调：onPathChanged() / buildObjPath()（广播剩余路点 + 寻路耗时，供客户端画线）
//  11. 工具函数：clampCell / buildWorldObject / buildObjMove / snapshotInTowers /
//      towerUids / pushToUIDs / pushObjEnterExcept / pushObjMoveToTower / pushObjTowerChange
package game

import (
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
	"github.com/gulurence/cherry-server/examples/login-gate/worldsim"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
)

const (
	tickInterval   = 50 * time.Millisecond
	npcPerFaction  = 200
	playerIdleWait = 300 * time.Second
)

// pathWorkerCount 是玩家 moveTo 全图寻路的 worker 池大小（每 worker 独立 ~16MB scratch）。
// 实测（8 物理核 / 16 逻辑核）：16 worker 优于 8 worker（wall p50 3.9s vs 4.5s），取满逻辑核。
var pathWorkerCount = 16

// 以下是与 ActorWorld 通信的 RPC 参数。它们只在同一 game 节点内以 Go 对象形式
// 传递，不经过线上序列化，但保留 json tag 以便未来可跨节点调用。
type simEnterArg struct {
	Uid       int64  `json:"uid"`
	AgentPath string `json:"agent_path"`
	Faction   int32  `json:"faction"`
}

type simMoveToArg struct {
	Uid       int64      `json:"uid"`
	AgentPath string     `json:"agent_path"` // 会话三字段，供 worldsim 直接延迟响应（绕过 ActorWorld）
	Sid       string     `json:"sid"`
	Mid       uint32     `json:"mid"`
	X         int32      `json:"x"`
	Y         int32      `json:"y"`
	Path      []msg.Cell `json:"path"` // 客户端预寻路路点（可选）
}

type simViewArg struct {
	Uid int64 `json:"uid"`
	X   int32 `json:"x"`
	Y   int32 `json:"y"`
}

type simLeaveArg struct {
	Uid int64 `json:"uid"`
}

// pendingMove 记录一次已入队、等待 worker 结果的玩家 moveTo，用于结果回填与延迟响应。
type pendingMove struct {
	uid       int64
	seq       int // 玩家当前 moveTo 序号，用于丢弃被更新请求取代的过期结果
	goal      worldsim.Vec2i
	agentPath string
	sid       string
	mid       uint32
}

// playerEntry 记录一个已进入世界的玩家的会话与对象关联。
type playerEntry struct {
	Uid       int64
	AgentPath string
	Faction   worldsim.Faction
	ObjID     int
	ViewX     int
	ViewY     int
	LastSeen  time.Time
	pathSeq   int // 该玩家已提交的 moveTo 序号（用于丢弃过期寻路结果）
}

// ActorWorldSim 是 game 节点上独立的世界模拟 actor(actorID = "worldsim")。
//
// 它拥有完整的世界状态：随机地图、NPC 游走、阵营碰撞、灯塔视野、周期 tick，
// 以及所有面向客户端的推送（objEnter / objMove / objLeave）。协议层 ActorWorld
// 通过 Call/CallWait 调用这里的远程函数，两者各跑在自己的 goroutine 上。
type ActorWorldSim struct {
	pomelo.ActorBase

	sim       *worldsim.Game
	mapEnc    string // 地形快照的 RLE+base64（进入世界时下发，静态、启动时编码一次）
	castleEnc string // 城堡阻挡快照的 RLE+base64，随城堡增删重建
	rng       *rand.Rand
	nextObjID int

	players  map[int64]*playerEntry
	objOwner map[int]int64 // objID -> uid（NPC 为 0）

	// 异步寻路：token -> 已入队待回填的 moveTo 请求（结果由 drainPathResults 回填）。
	pendingMove   map[int64]pendingMove
	nextMoveToken int64

	aoi      aoiState
	lastTick time.Time

	// 碰撞统计：按阵营关系聚合，避免 3000 个 NPC 时逐条日志刷屏。
	collStats   map[worldsim.Relation]int
	lastCollLog time.Time
}

// AliasID 是顶层 actor 的注册 id，最终路径为 "<gameNodeID>.worldsim"。
func (p *ActorWorldSim) AliasID() string {
	return "worldsim"
}

// OnInit 生成世界、注册远程函数并启动世界模拟。
func (p *ActorWorldSim) OnInit() {
	// 地图种子：默认用时间随机；设置 LG_MAP_SEED 环境变量可固定地图，便于压测前后对比。
	seed := time.Now().UnixNano()
	if v := os.Getenv("LG_MAP_SEED"); v != "" {
		if s, err := strconv.ParseInt(v, 10, 64); err == nil {
			seed = s
		}
	}
	p.rng = rand.New(rand.NewSource(seed))
	gm := worldsim.GenerateMap(p.rng)
	p.mapEnc = worldsim.EncodeGridBase64(gm.MapString)
	p.sim = worldsim.NewGame(gm.Grid, p.rng, pathWorkerCount)
	p.sim.OnCollision = p.onCollision
	p.sim.OnPathChanged = p.onPathChanged

	p.players = make(map[int64]*playerEntry)
	p.objOwner = make(map[int]int64)
	p.pendingMove = make(map[int64]pendingMove)
	p.aoi.init()
	p.nextObjID = 1
	p.lastTick = time.Now()
	p.collStats = make(map[worldsim.Relation]int)
	p.lastCollLog = p.lastTick

	p.spawnNPCs()

	// 预置动态城堡，使其初始即覆盖全图 1/8，之后由 tick 里的 UpdateCastles 每 5 秒增删。
	p.sim.SeedCastles()
	p.castleEnc = p.castleMapEncoded()

	p.Remote().Register("enter", p.enter)
	p.Remote().Register("moveTo", p.moveTo)
	p.Remote().Register("view", p.view)
	p.Remote().Register("leave", p.leave)

	p.Timer().Add(tickInterval, p.tick)
}

// ---- 世界初始化 ----

func (p *ActorWorldSim) spawnNPCs() {
	// 红 / 黑 / 中立（绿）各 200 个，共 600 个 NPC。
	for f := worldsim.FactionRed; f <= worldsim.FactionGreen; f++ {
		for i := 0; i < npcPerFaction; i++ {
			cell := p.randomWalkable()
			size := 1 + p.rng.Intn(3) // 随机 1x1 / 2x2 / 3x3
			o := &worldsim.Object{
				ID:       p.nextObjID,
				Faction:  f,
				IsPlayer: false,
				Size:     size,
				Radius:   worldsim.RadiusForSize(size),
				Pos:      worldsim.Vec2{X: float32(cell.X) + 0.5, Y: float32(cell.Y) + 0.5},
			}
			p.nextObjID++
			p.sim.AddObject(o)
			p.aoi.addObject(o.ID, cell.X, cell.Y)
			p.sim.AssignRandomTarget(o)
		}
	}
}

func (p *ActorWorldSim) randomWalkable() worldsim.Vec2i {
	for i := 0; i < 1000; i++ {
		c := worldsim.Vec2i{X: p.rng.Intn(worldsim.MapW), Y: p.rng.Intn(worldsim.MapH)}
		if p.sim.Grid().Walkable(c) {
			return c
		}
	}
	for y := 0; y < worldsim.MapH; y++ {
		for x := 0; x < worldsim.MapW; x++ {
			if p.sim.Grid().Walkable(worldsim.Vec2i{X: x, Y: y}) {
				return worldsim.Vec2i{X: x, Y: y}
			}
		}
	}
	return worldsim.Vec2i{}
}

// ---- 远程函数：被 ActorWorld 通过 Call/CallWait 调用 ----

func (p *ActorWorldSim) enter(req *simEnterArg) (*msg.EnterResponse, int32) {
	faction := worldsim.Faction(req.Faction)
	if faction < worldsim.FactionRed || faction > worldsim.FactionGreen {
		return nil, code.InvalidFaction
	}

	uid := req.Uid
	p.removePlayer(uid) // 重复进入：顶掉旧对象

	cell := p.randomWalkable()
	objID := p.nextObjID
	p.nextObjID++

	o := &worldsim.Object{
		ID:       objID,
		Faction:  faction,
		IsPlayer: true,
		Uid:      uid,
		Size:     1,
		Radius:   worldsim.RadiusForSize(1),
		Speed:    worldsim.PlayerMoveSpeed,
		Pos:      worldsim.Vec2{X: float32(cell.X) + 0.5, Y: float32(cell.Y) + 0.5},
	}
	p.sim.AddObject(o)
	p.aoi.addObject(objID, cell.X, cell.Y)

	// 向已订阅该塔的其他玩家广播 objEnter（自己尚未订阅，不会收到）
	p.pushObjEnterExcept(o, uid)

	entry := &playerEntry{
		Uid:       uid,
		AgentPath: req.AgentPath,
		Faction:   faction,
		ObjID:     objID,
		ViewX:     cell.X,
		ViewY:     cell.Y,
		LastSeen:  time.Now(),
	}
	p.players[uid] = entry
	p.objOwner[objID] = uid

	towers := p.aoi.subscribe(uid, cell.X, cell.Y)
	snapshot := p.snapshotInTowers(towers, objID)

	return &msg.EnterResponse{
		Map:     p.mapEnc,
		SelfID:  int64(objID),
		SpawnX:  int32(cell.X),
		SpawnY:  int32(cell.Y),
		Faction: int32(faction),
		Objects: snapshot,
		Castles: p.castleEnc,
	}, code.OK
}

func (p *ActorWorldSim) moveTo(req *simMoveToArg) (*msg.MoveToResponse, int32) {
	e, ok := p.players[req.Uid]
	if !ok {
		p.respondMove(req.AgentPath, req.Sid, req.Mid, code.NotEntered, 0, nil)
		return nil, code.OK
	}
	o := p.sim.Object(e.ObjID)
	if o == nil {
		p.respondMove(req.AgentPath, req.Sid, req.Mid, code.NotEntered, 0, nil)
		return nil, code.OK
	}

	target := worldsim.Vec2i{X: int(req.X), Y: int(req.Y)}
	if !p.sim.Grid().Walkable(target) {
		p.respondMove(req.AgentPath, req.Sid, req.Mid, code.TargetBlocked, 0, nil)
		return nil, code.OK
	}

	e.LastSeen = time.Now()

	// 客户端预寻路：保持同步 SetClientPath + 立即响应（不走 worker 池，本期不改）。
	if len(req.Path) > 0 {
		clientPath := make([]worldsim.Vec2i, len(req.Path))
		for i, c := range req.Path {
			clientPath[i] = worldsim.Vec2i{X: int(c.X), Y: int(c.Y)}
		}
		path := p.sim.SetClientPath(o, target, clientPath)
		if path == nil {
			p.respondMove(req.AgentPath, req.Sid, req.Mid, code.TargetBlocked, 0, nil)
			return nil, code.OK
		}
		p.respondMove(req.AgentPath, req.Sid, req.Mid, code.OK, o.PathCostMs, path)
		return nil, code.OK
	}

	// 服务器全量寻路：异步入队到 worker 池，结果由 tick 里的 drainPathResults 回填并响应。
	e.pathSeq++
	token := p.nextMoveToken
	p.nextMoveToken++
	p.pendingMove[token] = pendingMove{
		uid:       req.Uid,
		seq:       e.pathSeq,
		goal:      target,
		agentPath: req.AgentPath,
		sid:       req.Sid,
		mid:       req.Mid,
	}
	p.sim.SubmitPath(worldsim.PathJob{Token: token, Start: o.Cell, Goal: target, MaxExpand: 0})
	return nil, code.OK
}

// respondMove 把 moveTo 结果直接回给客户端（绕过 ActorWorld 的 CallWait）。
func (p *ActorWorldSim) respondMove(agentPath, sid string, mid uint32, errCode int32, costMs int64, path []worldsim.Vec2i) {
	if errCode != code.OK {
		pomelo.ResponseCode(p, agentPath, sid, mid, errCode)
		return
	}
	cells := make([]msg.Cell, 0, len(path))
	for _, c := range path {
		cells = append(cells, msg.Cell{X: int32(c.X), Y: int32(c.Y)})
	}
	pomelo.Response(p, agentPath, sid, mid, &msg.MoveToResponse{Code: code.OK, CostMs: int32(costMs), Path: cells})
}

// drainPathResults 把已就绪的异步寻路结果回填到对象并响应。由 tick 每帧调用。
func (p *ActorWorldSim) drainPathResults() {
	p.sim.PathPool().Drain(func(r worldsim.PathResult) {
		if r.Kind == worldsim.PathRepath {
			p.sim.ApplyRepathResult(r)
			return
		}
		pm, ok := p.pendingMove[r.Token]
		if !ok {
			return
		}
		delete(p.pendingMove, r.Token)

		e, ok := p.players[pm.uid]
		if !ok {
			return // 玩家已离开
		}
		if e.pathSeq != pm.seq {
			return // 已被更新的 moveTo 取代
		}
		o := p.sim.Object(e.ObjID)
		if o == nil {
			return
		}

		if r.Path == nil {
			p.respondMove(pm.agentPath, pm.sid, pm.mid, code.TargetBlocked, 0, nil)
			return
		}

		path := worldsim.RebasePath(o.Cell, r.Path)
		if path == nil {
			// 对象在寻路期间已移动超过 1 格：以当前格重新入队重算。
			e.pathSeq++
			token := p.nextMoveToken
			p.nextMoveToken++
			p.pendingMove[token] = pendingMove{pm.uid, e.pathSeq, pm.goal, pm.agentPath, pm.sid, pm.mid}
			p.sim.SubmitPath(worldsim.PathJob{Token: token, Start: o.Cell, Goal: pm.goal, MaxExpand: 0})
			return
		}

		o.Path = path
		o.PathIdx = 0
		o.Target = pm.goal
		o.State = worldsim.StateMoving
		o.PathCostMs = r.CostMs
		if p.sim.OnPathChanged != nil {
			p.sim.OnPathChanged(o)
		}
		p.respondMove(pm.agentPath, pm.sid, pm.mid, code.OK, r.CostMs, path)
	})
}

func (p *ActorWorldSim) view(req *simViewArg) int32 {
	e, ok := p.players[req.Uid]
	if !ok {
		return code.NotEntered
	}

	cx, cy := p.clampCell(int(req.X), int(req.Y))
	e.ViewX, e.ViewY = cx, cy
	e.LastSeen = time.Now()

	added, removed := p.aoi.resubscribe(e.Uid, cx, cy)

	// 新增塔：向该玩家逐个广播塔内对象 objEnter
	for _, t := range added {
		for objID := range p.aoi.towerObjs[t] {
			if objID == e.ObjID {
				continue
			}
			if o := p.sim.Object(objID); o != nil {
				p.pushToUIDs("game.world.objEnter", p.buildWorldObject(o), []int64{e.Uid})
			}
		}
	}
	// 移除塔：向该玩家逐个广播 objLeave
	for _, t := range removed {
		for objID := range p.aoi.towerObjs[t] {
			if objID == e.ObjID {
				continue
			}
			p.pushToUIDs("game.world.objLeave", &msg.ObjLeavePush{ID: int64(objID)}, []int64{e.Uid})
		}
	}

	// 新增塔：补发塔内当前城堡快照（灯塔视野切换时的城堡同步）。
	if len(added) > 0 {
		byTower := p.castleCellsByTower(added)
		for _, t := range added {
			p.pushToUIDs("game.world.castleTower",
				&msg.CastleTowerPush{Tower: int32(t), Cells: p.castleCellsMsg(byTower[t])},
				[]int64{e.Uid})
		}
	}

	return code.OK
}

func (p *ActorWorldSim) leave(req *simLeaveArg) int32 {
	p.removePlayer(req.Uid)
	return code.OK
}

// ---- 每帧模拟 ----

func (p *ActorWorldSim) tick() {
	now := time.Now()
	dt := float32(now.Sub(p.lastTick).Seconds())
	p.lastTick = now
	if dt <= 0 {
		dt = 0.05
	}
	if dt > 0.25 {
		dt = 0.25
	}

	// 动态城堡先增删，再跑模拟：Update 会因城堡挡住路径而为对象重新寻路。
	if added, removed := p.sim.UpdateCastles(now); len(added) > 0 || len(removed) > 0 {
		p.broadcastCastleCells("game.world.castleAdd", added)
		p.broadcastCastleCells("game.world.castleRemove", removed)
		p.castleEnc = p.castleMapEncoded()
	}

	p.sim.Update(dt)
	p.drainPathResults()
	p.sim.WanderNPCs(now)
	p.syncAOI()
	p.reportCollisions(now)
	p.sweepIdle(now)
}

// syncAOI 把模拟后的对象位置变化同步成灯塔事件。
func (p *ActorWorldSim) syncAOI() {
	for _, o := range p.sim.Objects() {
		newCell := o.Cell
		lastCell, tracked := p.aoi.objCell[o.ID]
		if !tracked {
			p.aoi.addObject(o.ID, newCell.X, newCell.Y)
			continue
		}
		if newCell == lastCell {
			continue // 未换格，无事件
		}

		oldTower := p.aoi.objTower[o.ID]
		newTower := p.aoi.towerKey(newCell.X, newCell.Y)
		p.aoi.moveObject(o.ID, newCell.X, newCell.Y)

		if newTower == oldTower {
			p.pushObjMoveToTower(o)
		} else {
			p.pushObjTowerChange(o, oldTower, newTower)
		}
	}
}

func (p *ActorWorldSim) sweepIdle(now time.Time) {
	for uid, e := range p.players {
		if now.Sub(e.LastSeen) > playerIdleWait {
			p.removePlayer(uid)
		}
	}
}

// removePlayer 移除玩家对象并清理 AOI / 订阅，同时向视野内其他玩家广播 objLeave。
func (p *ActorWorldSim) removePlayer(uid int64) {
	e, ok := p.players[uid]
	if !ok {
		return
	}
	if t, ok := p.aoi.objTower[e.ObjID]; ok {
		p.pushToUIDs("game.world.objLeave", &msg.ObjLeavePush{ID: int64(e.ObjID)}, p.towerUids(t, uid))
	}
	p.sim.RemoveObject(e.ObjID)
	p.aoi.removeObject(e.ObjID)
	p.aoi.unsubscribe(uid)
	delete(p.objOwner, e.ObjID)
	delete(p.players, uid)
}

// ---- 碰撞回调 ----

func (p *ActorWorldSim) onCollision(a, b *worldsim.Object, rel worldsim.Relation) {
	p.collStats[rel]++
}

// onPathChanged 在对象获得新路径时，向当前塔内订阅玩家广播路点 + 寻路耗时。
func (p *ActorWorldSim) onPathChanged(o *worldsim.Object) {
	t, ok := p.aoi.objTower[o.ID]
	if !ok {
		return
	}
	uids := p.towerUids(t, 0)
	if len(uids) == 0 {
		return
	}
	p.pushToUIDs("game.world.objPath", p.buildObjPath(o), uids)
}

// buildObjPath 将对象剩余路径转成协议结构（含寻路耗时）。
func (p *ActorWorldSim) buildObjPath(o *worldsim.Object) *msg.ObjPathPush {
	cells := make([]msg.Cell, 0, len(o.Path))
	for _, c := range o.Path[o.PathIdx:] {
		cells = append(cells, msg.Cell{X: int32(c.X), Y: int32(c.Y)})
	}
	return &msg.ObjPathPush{
		ID:     int64(o.ID),
		CostMs: int32(o.PathCostMs),
		Path:   cells,
	}
}

// reportCollisions 每秒汇总一次碰撞统计，避免逐条刷屏。
func (p *ActorWorldSim) reportCollisions(now time.Time) {
	if now.Sub(p.lastCollLog) < time.Second {
		return
	}
	p.lastCollLog = now
	if len(p.collStats) == 0 {
		return
	}
	clog.Infof("[world] collisions(1s): battle=%d hello=%d good=%d",
		p.collStats[worldsim.RelEnemy],
		p.collStats[worldsim.RelFriendly],
		p.collStats[worldsim.RelNeutral])
	p.collStats = make(map[worldsim.Relation]int)
}

// ---- 工具 ----

func (p *ActorWorldSim) clampCell(x, y int) (int, int) {
	if x < 0 {
		x = 0
	}
	if x >= worldsim.MapW {
		x = worldsim.MapW - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= worldsim.MapH {
		y = worldsim.MapH - 1
	}
	return x, y
}

func (p *ActorWorldSim) buildWorldObject(o *worldsim.Object) *msg.WorldObject {
	kind := int32(0)
	if o.IsPlayer {
		kind = 1
	}
	return &msg.WorldObject{
		ID:      int64(o.ID),
		Kind:    kind,
		Faction: int32(o.Faction),
		Size:    int32(o.Size),
		X:       o.Pos.X,
		Y:       o.Pos.Y,
		CellX:   int32(o.Cell.X),
		CellY:   int32(o.Cell.Y),
	}
}

func (p *ActorWorldSim) buildObjMove(o *worldsim.Object) *msg.ObjMovePush {
	return &msg.ObjMovePush{
		ID:    int64(o.ID),
		X:     o.Pos.X,
		Y:     o.Pos.Y,
		CellX: int32(o.Cell.X),
		CellY: int32(o.Cell.Y),
	}
}

// snapshotInTowers 返回这些塔内所有对象（去重，排除某个对象）的协议快照。
func (p *ActorWorldSim) snapshotInTowers(towers []int, exclude int) []msg.WorldObject {
	seen := make(map[int]struct{})
	out := make([]msg.WorldObject, 0)
	for _, t := range towers {
		for id := range p.aoi.towerObjs[t] {
			if id == exclude {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			if o := p.sim.Object(id); o != nil {
				out = append(out, *p.buildWorldObject(o))
			}
		}
	}
	return out
}

// towerUids 返回订阅某塔的所有玩家 uid（排除某个 uid）。
func (p *ActorWorldSim) towerUids(t int, exceptUid int64) []int64 {
	var uids []int64
	for uid := range p.aoi.towerPlayers[t] {
		if uid == exceptUid {
			continue
		}
		uids = append(uids, uid)
	}
	return uids
}

// castleMapString 把当前城堡阻挡格渲染成 1,000,000 字符的 '0'/'1' 串，'1' 表示城堡格。
// 客户端进入世界时一次性绘制；后续增删走 castleAdd / castleRemove 推送增量更新。
func (p *ActorWorldSim) castleMapString() string {
	b := make([]byte, worldsim.MapW*worldsim.MapH)
	for i := range b {
		b[i] = '0'
	}
	for _, c := range p.sim.AllCastleCells() {
		b[c.Y*worldsim.MapW+c.X] = '1'
	}
	return string(b)
}

// castleMapEncoded 返回城堡阻挡快照的 RLE+base64 下发形式（8×8 实心块，压缩率约 12×）。
func (p *ActorWorldSim) castleMapEncoded() string {
	return worldsim.EncodeGridBase64(p.castleMapString())
}

// castleCellsMsg 把一组城堡格子转成协议结构。
func (p *ActorWorldSim) castleCellsMsg(cells []worldsim.Vec2i) []msg.Cell {
	out := make([]msg.Cell, 0, len(cells))
	for _, c := range cells {
		out = append(out, msg.Cell{X: int32(c.X), Y: int32(c.Y)})
	}
	return out
}

// castleCellsByTower 返回这些塔各自的当前城堡格子（tower -> cells）。用于视野切换时
// 给玩家补发每个新增塔的完整城堡快照，客户端据此清空并重设该塔，避免因视野外
// 增删漏收导致的新旧城堡残留。
func (p *ActorWorldSim) castleCellsByTower(towers []int) map[int][]worldsim.Vec2i {
	out := make(map[int][]worldsim.Vec2i, len(towers))
	if len(towers) == 0 {
		return out
	}
	set := make(map[int]struct{}, len(towers))
	for _, t := range towers {
		set[t] = struct{}{}
		out[t] = nil
	}
	for _, c := range p.sim.AllCastleCells() {
		t := p.aoi.towerKey(c.X, c.Y)
		if _, ok := set[t]; ok {
			out[t] = append(out[t], c)
		}
	}
	return out
}

// broadcastCastleCells 用灯塔（AOI）把城堡格子增删推送给“视野覆盖到这些格子”的玩家，
// 而非广播给所有在线玩家。做法：先把格子按塔分组，每座塔只把塔内的格子推给订阅该塔
// 的玩家，保证每名玩家只收到自己视野内的城堡变化（严格灯塔扇出）。
func (p *ActorWorldSim) broadcastCastleCells(route string, cells []worldsim.Vec2i) {
	if len(cells) == 0 {
		return
	}
	byTower := make(map[int][]worldsim.Vec2i)
	for _, c := range cells {
		t := p.aoi.towerKey(c.X, c.Y)
		byTower[t] = append(byTower[t], c)
	}
	for t, towerCells := range byTower {
		p.pushToUIDs(route, &msg.CastleCellsPush{Cells: p.castleCellsMsg(towerCells)}, p.towerUids(t, 0))
	}
}

// pushToUIDs 按玩家所在网关（AgentPath）分组，向一批 uid 广播推送。
func (p *ActorWorldSim) pushToUIDs(route string, v any, uids []int64) {
	if len(uids) == 0 {
		return
	}
	groups := make(map[string][]int64)
	for _, uid := range uids {
		if e, ok := p.players[uid]; ok {
			groups[e.AgentPath] = append(groups[e.AgentPath], uid)
		}
	}
	for agentPath, list := range groups {
		p.PushWithUIDS(agentPath, list, false, route, v)
	}
}

func (p *ActorWorldSim) pushObjEnterExcept(o *worldsim.Object, exceptUid int64) {
	if t, ok := p.aoi.objTower[o.ID]; ok {
		p.pushToUIDs("game.world.objEnter", p.buildWorldObject(o), p.towerUids(t, exceptUid))
	}
}

func (p *ActorWorldSim) pushObjMoveToTower(o *worldsim.Object) {
	if t, ok := p.aoi.objTower[o.ID]; ok {
		p.pushToUIDs("game.world.objMove", p.buildObjMove(o), p.towerUids(t, 0))
	}
}

// pushObjTowerChange 对象换塔时，向新旧塔的订阅者分别发送 enter/leave/move。
func (p *ActorWorldSim) pushObjTowerChange(o *worldsim.Object, oldTower, newTower int) {
	oldSet := make(map[int64]struct{})
	for _, u := range p.towerUids(oldTower, 0) {
		oldSet[u] = struct{}{}
	}
	newSet := make(map[int64]struct{})
	for _, u := range p.towerUids(newTower, 0) {
		newSet[u] = struct{}{}
	}

	var enterTo, leaveTo, moveTo []int64
	for u := range newSet {
		if _, ok := oldSet[u]; ok {
			moveTo = append(moveTo, u)
		} else {
			enterTo = append(enterTo, u)
		}
	}
	for u := range oldSet {
		if _, ok := newSet[u]; !ok {
			leaveTo = append(leaveTo, u)
		}
	}

	p.pushToUIDs("game.world.objEnter", p.buildWorldObject(o), enterTo)
	p.pushToUIDs("game.world.objMove", p.buildObjMove(o), moveTo)
	p.pushToUIDs("game.world.objLeave", &msg.ObjLeavePush{ID: int64(o.ID)}, leaveTo)
}
