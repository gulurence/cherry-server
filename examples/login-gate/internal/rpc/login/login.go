// Package rpcLogin 封装"网关 -> 登录服"的跨节点 RPC 调用。
//
// 调用方(网关)只需要传 IApplication 和账号密码，不需要关心登录服具体是哪个节点、
// 登录服账号 actor 的路径长什么样。这些都封装在本文件里。
package rpcLogin

import (
	cfacade "github.com/gulurence/cherry-server/facade"
	clog "github.com/gulurence/cherry-server/logger"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
)

const (
	loginType    = "login"   // 登录服的节点类型
	accountActor = ".account" // 登录服账号 actor 的 id
	verifyFunc   = "verify"   // 账号校验的远程函数名

	// sourcePath 是调用方的"来源路径"。跨节点 RPC 需要一个合法的 source 路径，
	// 这里用一个不指向任何真实 actor 的占位路径(与框架示例一致)。
	sourcePath = ".system"
)

// Verify 调用登录服校验账号密码，返回 (uid + 令牌, 错误码)。
//
// 错误码非 code.OK 时，resp 为 nil，调用方应直接把错误码回给客户端。
func Verify(app cfacade.IApplication, account, password string) (*msg.LoginResponse, int32) {
	nodeID := GetLoginNodeID(app)
	if nodeID == "" {
		clog.Warn("[Verify] login node not found.")
		return nil, code.LoginError
	}

	req := &msg.LoginRequest{Account: account, Password: password}
	rsp := &msg.LoginResponse{}

	// 跨节点同步调用：targetPath = "登录服节点id.account"，远程函数 = verify。
	errCode := app.ActorSystem().CallWait(sourcePath, nodeID+accountActor, verifyFunc, req, rsp)
	if code.IsFail(errCode) {
		clog.Warnf("[Verify] account = %s, errCode = %d", account, errCode)
		return nil, errCode
	}

	return rsp, code.OK
}

// GetLoginNodeID 从发现服务里取一个登录服节点 id。
// 示例中登录服只有一个，直接取列表第一个；多登录服时可按负载均衡策略选择。
func GetLoginNodeID(app cfacade.IApplication) string {
	list := app.Discovery().ListByType(loginType)
	if len(list) > 0 {
		return list[0].GetNodeID()
	}
	return ""
}
