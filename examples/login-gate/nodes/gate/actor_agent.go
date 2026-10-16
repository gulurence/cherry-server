// ActorAgent：每条客户端连接对应一个实例，是"这条连接"在网关侧的全部状态。
//
// 创建时机见 gate.go 的 SetOnNewAgent：
//
//	agentActor.Child().Create(newAgent.SID(), childActor)   // actorID == sid
//
// 于是路径是 "<节点id>.user.<sid>"。一连接一 actor 的好处：
//   - actor 内部字段天然串行访问，不需要为每个连接加锁
//   - 玩家断线时 actor 退出即可自动清理资源
package gate

import (
	clog "github.com/gulurence/cherry-server/logger"
	cactor "github.com/gulurence/cherry-server/net/actor"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	cproto "github.com/gulurence/cherry-server/net/proto"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
	rpcLogin "github.com/gulurence/cherry-server/examples/login-gate/internal/rpc/login"
)

var (
	// duplicateLoginCode "顶号"时发给旧连接的踢人包内容，在 gate.Run() 里预计算。
	duplicateLoginCode []byte
)

// ActorAgent 每个网络连接对应一个 ActorAgent。
type ActorAgent struct {
	cactor.Base
}

// OnInit 注册本连接需要处理的消息。
func (p *ActorAgent) OnInit() {
	// Local：客户端消息经路由后投递进来(route.go 里的 LocalDataRoute)。
	p.Local().Register("login", p.login)
}

// login 用户登录，验证账号 (route: gate.user.login)。
//
// 这是客户端连上网关后必须发送的第一条协议，做三件事：
//  1. 校验请求参数
//  2. 跨节点调用 login 节点校验账号，换取 uid + 令牌
//  3. 把 uid 绑定到这条连接，并处理"顶号"
func (p *ActorAgent) login(session *cproto.Session, req *msg.LoginRequest) {
	agent, found := pomelo.GetAgent(p.ActorID(), 0)
	if !found {
		return
	}

	if req == nil || req.Account == "" || req.Password == "" {
		agent.ResponseCode(session, code.LoginError, true)
		return
	}

	// 跨节点 RPC：调用登录服校验账号。会阻塞本 actor 直到登录服返回。
	rsp, errCode := rpcLogin.Verify(p.App(), req.Account, req.Password)
	if code.IsFail(errCode) {
		agent.ResponseCode(session, errCode, true)
		return
	}

	// 把 uid 绑定到这条连接。如果该 uid 之前已在别的连接登录，oldAgent 就是旧连接(顶号)。
	oldAgent, err := pomelo.Bind(session.Sid, rsp.UID)
	if err != nil {
		agent.ResponseCode(session, code.LoginError, true)
		clog.Warnf("[login] bind uid fail. sid = %s, uid = %d, err = %v", session.Sid, rsp.UID, err)
		return
	}

	// 挤掉之前的连接(同一个账号重复登录)。
	if oldAgent != nil {
		oldAgent.Kick(duplicateLoginCode, true)
	}

	clog.Infof("[login] account = %s, uid = %d, sid = %s", req.Account, rsp.UID, session.Sid)

	// 回包：uid + 令牌
	agent.Response(session, rsp)
}

// onSessionClose 连接断开时回调，清理对应的 ActorAgent。
//
// 在 gate.go 的 SetOnNewAgent 里通过 newAgent.AddOnClose 注册。
// 注意这里运行在连接收尾路径上，只做轻量清理，不要做耗时操作。
func (p *ActorAgent) onSessionClose(_ *pomelo.Agent) {
	p.Exit()
	clog.Infof("[sessionClose] path = %s", p.Path())
}
