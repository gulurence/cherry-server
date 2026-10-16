// Package game 是 game 节点(后端进程)。
//
// 职责：承载 world 与 player 两类 actor。
//   - world：game 节点上的父 actor，处理世界级协议，并作为 player 的父 actor
//   - player：每个 uid 一个的子 actor，处理玩家级协议
//
// 它不接受客户端连接；客户端消息由网关跨节点转发进来(走 PublishLocal 本地通道)，
// 面向客户端的响应则通过 session 里的 AgentPath 回传给网关(见 pomelo.ActorBase.Response)。
package game

import (
	"github.com/gulurence/cherry-server"
	cserializer "github.com/gulurence/cherry-server/net/serializer"
)

// Run 启动 game 节点。
func Run(profileFilePath, nodeID string) {
	app := cherry.Configure(
		profileFilePath,
		nodeID,
		false, // 非前端节点，不接收客户端连接
		cherry.Cluster,
	)

	// 全集群统一使用 JSON 序列化。
	app.SetSerializer(cserializer.NewJSON())

	// world actor 是唯一在启动时创建的顶层 actor；player 按 uid 在运行时按需创建。
	app.AddActors(&ActorWorld{})

	app.Startup()
}
