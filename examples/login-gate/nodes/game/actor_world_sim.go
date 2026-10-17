package game

import (
	"math/rand"
	"time"

	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
	"github.com/gulurence/cherry-server/examples/login-gate/worldsim"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
)

const (
	tickInterval   = 50 * time.Millisecond
	npcCount       = 30
	playerIdleWait = 60 * time.Second
)

// 以下是与 ActorWorld 通信的 RPC 参数。它们只在同一 game 节点内以 Go 对象形式
// 传递，不经过线上序列化，但保留 json tag 以便未来可跨节点调用。
type simEnterArg struct {
	Uid       int64  `json:"uid"`
	AgentPath string `json:"agent_path"`
	Faction   int32  `json:"faction"`
}

type simMoveToArg struct {
	Uid int64 `json:"uid"`
	X   int32 `json:"x"`
	Y   int32 `json:"y"`
}

type simViewArg struct {
	Uid int64 `json:"uid"`
	X   int32 `json:"x"`
	Y   int32 `json:"y"`
}

type simLeaveArg struct {
	Uid int64 `json:"uid"`
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
}

// ActorWorldSim 是 game 节点上独立的世界模拟 actor(actorID = "worldsim")。
//
// 它拥有完整的世界状态：随机地图、NPC 游走、阵营碰撞、灯塔视野、周期 tick，
// 以及所有面向客户端的推送（objEnter / objMove / objLeave）。协议层 ActorWorld
// 通过 Call/CallWait 调用这里的远程函数，两者各跑在自己的 goroutine 上。
type ActorWorldSim struct {
	pomelo.ActorBase

	sim       *worldsim.Game
	mapStr    string
	rng       *rand.Rand
	nextObjID int

	players  map[int64]*playerEntry
	objOwner map[int]int64 // objID -> uid（NPC 为 0）

	aoi      aoiState
	lastTick time.Time
}

// AliasID 是顶层 actor 的注册 id，最终路径为 "<gameNodeID>.worldsim"。
func (p *ActorWorldSim) AliasID() string {
	return "worldsim"
}

// OnInit 生成世界、注册远程函数并启动世界模拟。
func (p *ActorWorldSim) OnInit() {
	p.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	gm := worldsim.GenerateMap(p.rng)
	p.mapStr = gm.MapString
	p.sim = worldsim.NewGame(gm.Grid, p.rng)
	p.sim.OnCollision = p.onCollision

	p.players = make(map[int64]*playerEntry)
	p.objOwner = make(map[int]int64)
	p.aoi.init()
	p.nextObjID = 1
	p.lastTick = time.Now()

	p.spawnNPCs()

	p.Remote().Register("enter", p.enter)
	p.Remote().Register("moveTo", p.moveTo)
	p.Remote().Register("view", p.view)
	p.Remote().Register("leave", p.leave)

	p.Timer().Add(tickInterval, p.tick)
}

// ---- 世界初始化 ----

func (p *ActorWorldSim) spawnNPCs() {
	for i := 0; i < npcCount; i++ {
		cell := p.randomWalkable()
		o := &worldsim.Object{
			ID:       p.nextObjID,
			Faction:  worldsim.Faction(p.rng.Intn(3)),
			IsPlayer: false,
			Pos:      worldsim.Vec2{X: float32(cell.X) + 0.5, Y: float32(cell.Y) + 0.5},
		}
		p.nextObjID++
		p.sim.AddObject(o)
		p.aoi.addObject(o.ID, cell.X, cell.Y)
		p.sim.AssignRandomTarget(o)
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
		Map:     p.mapStr,
		SelfID:  int64(objID),
		SpawnX:  int32(cell.X),
		SpawnY:  int32(cell.Y),
		Faction: int32(faction),
		Objects: snapshot,
	}, code.OK
}

func (p *ActorWorldSim) moveTo(req *simMoveToArg) (*msg.MoveToResponse, int32) {
	e, ok := p.players[req.Uid]
	if !ok {
		return nil, code.NotEntered
	}
	o := p.sim.Object(e.ObjID)
	if o == nil {
		return nil, code.NotEntered
	}

	target := worldsim.Vec2i{X: int(req.X), Y: int(req.Y)}
	if !p.sim.Grid().Walkable(target) {
		return nil, code.TargetBlocked
	}

	path := p.sim.SetPath(o, target)
	if path == nil {
		return nil, code.TargetBlocked
	}

	e.LastSeen = time.Now()
	cells := make([]msg.Cell, 0, len(path))
	for _, c := range path {
		cells = append(cells, msg.Cell{X: int32(c.X), Y: int32(c.Y)})
	}
	return &msg.MoveToResponse{Code: code.OK, Path: cells}, code.OK
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

	p.sim.Update(dt)
	p.sim.WanderNPCs()
	p.syncAOI()
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
	word := "good"
	switch rel {
	case worldsim.RelFriendly:
		word = "hello"
	case worldsim.RelEnemy:
		word = "battle"
	}
	clog.Infof("[world] collision=%s objA=%d(faction=%d) objB=%d(faction=%d)", word, a.ID, a.Faction, b.ID, b.Faction)
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
	var out []msg.WorldObject
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
