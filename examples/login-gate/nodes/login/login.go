// Package login 是登录节点(后端进程)。
//
// 职责：校验账号密码、签发登录令牌。它不接受客户端连接，只通过 NATS 向网关
// 提供 RPC。可以横向扩展多个实例，但示例里的账号存储在内存里，多实例之间不共享。
package login

import (
	"github.com/gulurence/cherry-server"
	cserializer "github.com/gulurence/cherry-server/net/serializer"
)

// Run 启动登录节点。
func Run(profileFilePath, nodeID string) {
	app := cherry.Configure(
		profileFilePath,
		nodeID,
		false, // 非前端节点
		cherry.Cluster,
	)

	// 全集群统一使用 JSON 序列化，避免引入 protobuf 代码生成步骤。
	app.SetSerializer(cserializer.NewJSON())

	// 账号 actor：账号存储与令牌密钥在启动时注入。
	app.AddActors(&ActorAccount{
		store:       newAccountStore(),
		tokenSecret: app.Settings().GetString("token_secret", "cherry-login-secret"),
	})

	app.Startup()
}
