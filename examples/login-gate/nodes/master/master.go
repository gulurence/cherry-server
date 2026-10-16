// Package master 是发现服务节点。
//
// master 不处理任何业务，只维护"集群里有哪些节点"。其它节点启动后向它注册，
// 并每秒发一次心跳；master 会剔除心跳超时的节点。
//
// 一个集群里只能有一个 master，并且必须最先启动(其它节点连不上 master 会一直重试)。
package master

import (
	"github.com/gulurence/cherry-server"
	cserializer "github.com/gulurence/cherry-server/net/serializer"
)

// Run 启动发现服务节点。
func Run(profileFilePath, nodeID string) {
	app := cherry.Configure(
		profileFilePath,
		nodeID,
		false, // 非前端节点，不接收客户端连接
		cherry.Cluster,
	)

	// 全集群统一使用 JSON 序列化，master 也要一致，否则发现服务的成员消息无法互通。
	app.SetSerializer(cserializer.NewJSON())

	app.Startup()
}
