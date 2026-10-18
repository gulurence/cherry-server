package game

import (
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
	cfacade "github.com/gulurence/cherry-server/facade"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	cproto "github.com/gulurence/cherry-server/net/proto"
)

// worldsimActorID 是承载世界模拟的独立顶层 actor 的 id（同一 game 节点上）。
const worldsimActorID = "worldsim"

// ActorWorld 是 game 节点上的父 actor(actorID = "world")。
//
// 职责：
//  1. 承载 world 级协议（enter / moveTo / view / leave），转发给独立的世界模拟 actor
//  2. 作为 player actor 的父 actor，按需创建 player 子 actor
//
// 世界模拟（随机地图、NPC 游走、阵营碰撞、灯塔视野、周期 tick）由独立的
// ActorWorldSim 承载，两者通过 Call/CallWait 通信，各自跑在自己的 goroutine 上。
type ActorWorld struct {
	pomelo.ActorBase
}

// AliasID 是顶层 actor 的注册 id，最终路径为 "<gameNodeID>.world"。
func (p *ActorWorld) AliasID() string {
	return "world"
}

// OnInit 注册 world 级协议。
func (p *ActorWorld) OnInit() {
	p.Local().Register("enter", p.enter)
	p.Local().Register("moveTo", p.moveTo)
	p.Local().Register("view", p.view)
	p.Local().Register("leave", p.leave)
}

// OnFindChild 按需创建 player 子 actor。
func (p *ActorWorld) OnFindChild(m *cfacade.Message) (cfacade.IActor, bool) {
	childID := m.TargetPath().ChildID
	if childID == "" {
		return nil, false
	}
	child, err := p.Child().Create(childID, &ActorPlayer{})
	if err != nil {
		clog.Warnf("[world] create player actor fail. childID = %s, err = %v", childID, err)
		return nil, false
	}
	return child, true
}

// worldSimPath 返回同一 game 节点上世界模拟 actor 的完整路径。
func (p *ActorWorld) worldSimPath() string {
	return p.NewNodePath(worldsimActorID)
}

// ---- 协议处理：薄转发，具体逻辑与推送都在 ActorWorldSim ----

func (p *ActorWorld) enter(session *cproto.Session, req *msg.EnterRequest) {
	rsp := &msg.EnterResponse{}
	errCode := p.CallWait(p.worldSimPath(), "enter", &simEnterArg{
		Uid:       session.Uid,
		AgentPath: session.AgentPath,
		Faction:   req.Faction,
	}, rsp)
	if code.IsFail(errCode) {
		p.ResponseCode(session, errCode)
		return
	}
	p.Response(session, rsp)
}

func (p *ActorWorld) moveTo(session *cproto.Session, req *msg.MoveToRequest) {
	// 异步转发：worldsim 校验后把全图 A* 交给 worker 池，结果由 worldsim 直接回给客户端
	// （Response 与 Call 解耦）。这里用 fire-and-forget 的 Call，不再 CallWait 阻塞，也不在此响应。
	p.Call(p.worldSimPath(), "moveTo", &simMoveToArg{
		Uid:       session.Uid,
		AgentPath: session.AgentPath,
		Sid:       session.Sid,
		Mid:       session.Mid,
		X:         req.X,
		Y:         req.Y,
		Path:      req.Path,
	})
}

func (p *ActorWorld) view(session *cproto.Session, req *msg.ViewRequest) {
	errCode := p.CallWait(p.worldSimPath(), "view", &simViewArg{
		Uid: session.Uid,
		X:   req.X,
		Y:   req.Y,
	}, nil)
	if code.IsFail(errCode) {
		p.ResponseCode(session, errCode)
		return
	}
	p.Response(session, &msg.ViewResponse{Code: code.OK})
}

func (p *ActorWorld) leave(session *cproto.Session, _ *msg.EmptyRequest) {
	p.Call(p.worldSimPath(), "leave", &simLeaveArg{Uid: session.Uid})
	p.Response(session, &msg.ViewResponse{Code: code.OK})
}
