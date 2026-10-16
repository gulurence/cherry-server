package game

import (
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	cproto "github.com/gulurence/cherry-server/net/proto"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
)

// ActorPlayer 是每个 uid 对应的 player 子 actor(路径 "<gameNodeID>.world.<uid>")。
//
// 由 ActorWorld.OnFindChild 按需创建。所有针对该玩家的协议都会串行投递到这个 actor，
// 因此 actor 内部字段天然并发安全，无需加锁。
type ActorPlayer struct {
	pomelo.ActorBase
}

// OnInit 注册玩家级协议。
func (p *ActorPlayer) OnInit() {
	// TODO: 在这里注册玩家业务协议。
	p.Local().Register("getInfo", p.getInfo)
}

// getInfo 查询玩家信息的占位实现。TODO: 填充玩家业务逻辑。
func (p *ActorPlayer) getInfo(session *cproto.Session, _ *msg.EmptyRequest) {
	p.Response(session, &msg.PlayerInfo{UID: session.Uid})
}
