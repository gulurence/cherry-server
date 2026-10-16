package login

import (
	cactor "github.com/gulurence/cherry-server/net/actor"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/token"
)

// ActorAccount 是登录服的账号 actor，对网关提供校验账号、签发令牌等远程函数。
//
// 登录服是后端节点，不直接面对客户端，所以只注册 Remote(RPC) 函数。
// 网关通过 internal/rpc/login 里的封装调用这些函数。
type ActorAccount struct {
	cactor.Base

	store       *accountStore // 账号存储，在 Run() 里注入
	tokenSecret string        // 令牌签名密钥，与网关保持一致
}

// AliasID 是 actor 的注册 id，远程调用路径为 "<登录服节点id>.account"。
func (p *ActorAccount) AliasID() string {
	return "account"
}

// OnInit 注册远程函数。返回值约定：
//
//	func(req) int32                        —— 只返回错误码
//	func(req) (resp, int32)                —— 返回响应体 + 错误码
//	func(req) (*msg.Xxx, int32)            —— 示例采用这种，resp 会被序列化带回调用方
func (p *ActorAccount) OnInit() {
	p.Remote().Register("verify", p.verify)
	p.Remote().Register("register", p.register)
}

// verify 校验账号密码，成功返回 (uid + 令牌, code.OK)。
func (p *ActorAccount) verify(req *msg.LoginRequest) (*msg.LoginResponse, int32) {
	if req == nil || req.Account == "" || req.Password == "" {
		return nil, code.LoginError
	}

	uid := p.store.Verify(req.Account, req.Password)
	if uid < 1 {
		return nil, code.AccountPasswordError
	}

	rsp := &msg.LoginResponse{
		UID:   uid,
		Token: token.New(uid, p.tokenSecret).String(),
	}
	return rsp, code.OK
}

// register 注册账号，返回 (uid, code)。
func (p *ActorAccount) register(req *msg.LoginRequest) (*msg.LoginResponse, int32) {
	if req == nil || req.Account == "" || req.Password == "" {
		return nil, code.LoginError
	}

	uid, ok := p.store.Register(req.Account, req.Password)
	if !ok {
		return nil, code.AccountExists
	}

	return &msg.LoginResponse{UID: uid}, code.OK
}
