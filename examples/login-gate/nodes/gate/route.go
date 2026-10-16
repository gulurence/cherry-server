// 网关的消息路由规则。
//
// 客户端发来的每条业务消息都会先进 onDataRoute，由它决定"这条消息去哪"：
//   - gate.*  本节点消息(登录等)  -> 本地路由到 <gateNodeID>.<handle>.<sid>
//   - game.*  游戏消息            -> 跨节点路由到 game 节点的 world / world.<uid>
//   - 其它    交给框架默认路由
package gate

import (
	cfacade "github.com/gulurence/cherry-server/facade"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	pmessage "github.com/gulurence/cherry-server/net/parser/pomelo/message"
	cproto "github.com/gulurence/cherry-server/net/proto"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
)

var (
	// firstRouteName 是客户端连上后必须发送的第一条协议(登录)。
	firstRouteName = "gate.user.login"

	// notLoginRsp 拒绝未登录连接时回给客户端的固定数据(错误码)。
	notLoginRsp = &msg.Int32{Value: code.AccountNotLogin}
)

const (
	// gameNodeType 是承载 world/player actor 的后端节点类型。
	gameNodeType = "game"

	// worldActorID 是 world actor 的 id，也是 player actor 的父 actor id。
	worldActorID = "world"
)

// onDataRoute 数据路由规则。
//
// 登录逻辑：
//  1. 客户端建立连接，服务端对应创建一个 ActorAgent 用于处理玩家消息(actorID == sid)
//  2. 客户端发送 gate.user.login 完成账号登录验证，通过后把 uid 绑定到当前 sid
//  3. 之后的消息必须携带已绑定的 uid，否则踢掉连接
func onDataRoute(agent *pomelo.Agent, route *pmessage.Route, msg *pmessage.Message) {
	// 每条消息一个 session 快照，里面带着这条连接的 sid/uid。
	session := agent.NewSession(msg.ID)

	// 第一道门禁：除了登录协议本身，没有绑定 uid 的连接一律拒绝。
	if !session.IsBind() && msg.Route != firstRouteName {
		agent.Kick(notLoginRsp, true)
		return
	}

	// 网关本节点的协议(登录等)：本地路由到 <gateNodeID>.<handle>.<sid>。
	if route.NodeType() == agent.NodeType() {
		targetPath := cfacade.NewChildPath(agent.NodeID(), route.HandleName(), session.Sid)
		pomelo.LocalDataRoute(agent, route, msg, targetPath)
		return
	}

	// game 节点的协议：跨节点路由到 world 或 world.<uid>。
	if route.NodeType() == gameNodeType {
		routeToGame(agent, route, msg, session)
		return
	}

	// 其它节点类型交给框架默认路由。
	pomelo.DefaultDataRoute(agent, route, msg)
}

// routeToGame 把玩家消息从网关转发到 game 节点。
//
// 路由约定：
//   game.world.*   -> <gameNodeID>.world        (world actor)
//   game.player.*  -> <gameNodeID>.world.<uid>  (player 子 actor)
func routeToGame(agent *pomelo.Agent, route *pmessage.Route, msg *pmessage.Message, session *cproto.Session) {
	member, found := agent.Discovery().Random(gameNodeType)
	if !found {
		clog.Warnf("[routeToGame] game node not found. route = %s", msg.Route)
		return
	}

	var targetPath string
	if route.HandleName() == worldActorID {
		// world 消息：目标是 world actor 本身。
		targetPath = cfacade.NewPath(member.GetNodeID(), worldActorID)
	} else {
		// player 消息：目标是 world 下 uid 对应的 player 子 actor。
		targetPath = cfacade.NewChildPath(member.GetNodeID(), worldActorID, session.Uid)
	}

	if err := pomelo.ClusterLocalDataRoute(agent, route, msg, member.GetNodeID(), targetPath); err != nil {
		clog.Warnf("[routeToGame] cluster route fail. route = %s, err = %v", msg.Route, err)
	}
}
