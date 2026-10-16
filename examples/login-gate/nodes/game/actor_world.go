package game

import (
	cfacade "github.com/gulurence/cherry-server/facade"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	cproto "github.com/gulurence/cherry-server/net/proto"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
)

// ActorWorld 是 game 节点上的父 actor(actorID = "world")。
//
// 职责：
//  1. 承载 world 级别协议(示例里是占位函数 enter)
//  2. 作为 player actor 的父 actor，按需创建每个 uid 对应的 player 子 actor
//
// 注意：玩家消息由网关跨节点转发进来，走的是本地消息通道(PublishLocal)，
// 所以面向客户端的 handler 用 Local().Register 注册，签名是 (session, req)。
type ActorWorld struct {
	pomelo.ActorBase
}

// AliasID 是顶层 actor 的注册 id，最终路径为 "<gameNodeID>.world"。
func (p *ActorWorld) AliasID() string {
	return "world"
}

// OnInit 注册 world 级别的协议。
func (p *ActorWorld) OnInit() {
	// TODO: 在这里注册 world 级别的业务协议。
	p.Local().Register("enter", p.enter)
}

// enter 进入 world 的占位实现。TODO: 填充进入世界/初始化逻辑。
func (p *ActorWorld) enter(session *cproto.Session, _ *msg.EmptyRequest) {
	p.Response(session, &msg.WorldInfo{WorldID: p.ActorID()})
}

// OnFindChild 按需创建 player 子 actor。
//
// 客户端消息被网关路由到 "<gameNodeID>.world.<uid>"，框架在本 actor 收到消息后
// 会调用 OnFindChild 解析子 actor；这里用 uid 作为 childID 创建(或复用) player actor。
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
