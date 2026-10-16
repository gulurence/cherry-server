// Package gate 是网关节点(前端进程)。
//
// 职责：唯一直接面对客户端连接的业务节点。它监听 tcp / websocket 两个端口，
// 解析 pomelo 协议，为每条连接创建一个 ActorAgent，并把登录请求转发给 login 节点。
package gate

import (
	"time"

	"github.com/gulurence/cherry-server"
	cfacade "github.com/gulurence/cherry-server/facade"
	cconnector "github.com/gulurence/cherry-server/net/connector"
	"github.com/gulurence/cherry-server/net/parser/pomelo"
	cserializer "github.com/gulurence/cherry-server/net/serializer"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/code"
	"github.com/gulurence/cherry-server/examples/login-gate/internal/msg"
)

// Run 启动网关节点。
func Run(profileFilePath, nodeID string) {
	app := cherry.Configure(
		profileFilePath,
		nodeID,
		true, // 前端节点，必须设置 netParser
		cherry.Cluster,
	)

	app.SetSerializer(cserializer.NewJSON())

	// 预计算"顶号"踢人包。它只取决于固定错误码，节点启动时算一次即可。
	// 不要放到 ActorAgent.OnInit 里算 —— 每个连接都会并发创建 ActorAgent，
	// 在那里写包级变量是数据竞争。
	duplicateLoginCode, _ = app.Serializer().Marshal(&msg.Int32{Value: code.AccountDuplicateLogin})

	app.SetNetParser(buildParser(app))

	app.Startup()
}

// buildParser 构建 pomelo 解析器：挂上连接器、设置新连接回调与数据路由。
func buildParser(app *cherry.AppBuilder) cfacade.INetParser {
	// "user" 既是解析器 actor 的 actorID，也是每条连接子 actor 的父节点名。
	agentActor := pomelo.NewActor("user")

	// websocket 监听地址取自配置里本节点的 address；tcp 监听地址取自节点配置。
	agentActor.AddConnector(cconnector.NewWS(app.Address()))
	agentActor.AddConnector(cconnector.NewTCP(app.Settings().GetString("tcp_address", ":20011")))

	// 新连接到来时，为它创建一个 ActorAgent 子 actor(actorID == sid)。
	agentActor.SetOnNewAgent(func(newAgent *pomelo.Agent) {
		childActor := &ActorAgent{}
		newAgent.AddOnClose(childActor.onSessionClose)
		agentActor.Child().Create(newAgent.SID(), childActor)
	})

	// 自定义数据路由：决定每条客户端消息去哪里。
	agentActor.SetOnDataRoute(onDataRoute)

	// 心跳间隔，客户端应发送心跳包的频率参照握手返回的 heartbeat 字段。
	agentActor.SetHeartbeat(60 * time.Second)

	return agentActor
}
