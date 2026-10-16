# login-gate 示例

用 cherry 引擎编写的最小分布式登录 + 游戏链路，包含四个进程：

| 进程 | 节点类型 | 职责 |
|------|----------|------|
| `master` | 发现服务 | 维护集群节点列表，不处理业务 |
| `login`  | 登录服   | 校验账号密码、签发登录令牌（后端，不接客户端） |
| `gate`   | 网关     | 面向客户端，管理连接，把登录/游戏消息转发给后端（前端） |
| `game`   | 游戏服   | 承载 world / player 两类 actor（后端，不接客户端） |

## 目录结构

```
login-gate/
├── main.go                  # 入口，按子命令启动 master / login / gate / game
├── config/
│   └── login-gate.json      # profile 配置（NATS 发现 + 节点定义）
├── internal/
│   ├── code/                # 业务错误码
│   ├── msg/                 # 消息结构体（JSON 序列化，无需 protobuf 代码生成）
│   ├── token/               # 登录令牌签发与校验
│   └── rpc/login/           # 网关 -> 登录服 的 RPC 封装
└── nodes/
    ├── master/              # 发现服务节点
    ├── login/               # 登录节点（账号存储 + account actor）
    ├── gate/                # 网关节点（pomelo 解析器 + ActorAgent + 路由）
    └── game/                # 游戏节点（world actor + player 子 actor）
```

## 消息链路

```
客户端 ──(pomelo/JSON)──> gate ──(NATS RPC)──> login
         gate.user.login       account.verify
          {account,password}    {account,password}
                               ─────────────>
                                {uid, token}
         <── {uid, token}
gate 把 uid 绑定到连接。

客户端 ──(pomelo/JSON)──> gate ──(NATS publish)──> game
         game.world.enter        world / world.<uid>
         game.player.getInfo
```

## 运行

1. 启动一个 NATS 服务（默认地址 `nats://127.0.0.1:4222`）：

   ```bash
   docker run -d --name nats -p 4222:4222 nats:latest
   ```

2. 按顺序启动四个进程（在 `examples/login-gate` 目录下）：

   ```bash
   go run . master --node=lg-master
   go run . login  --node=lg-login-1
   go run . gate   --node=lg-gate-1
   go run . game   --node=lg-game-1
   ```

3. 测试：用兼容 pomelo 协议的客户端连 `ws://127.0.0.1:20010`（或 tcp `:20011`），
   完成握手后发送：

   ```
   # 登录
   route: gate.user.login
   data : {"account":"admin","password":"admin123"}
   # => {"uid":10001,"token":"<base64 令牌>"}

   # 进入世界（world actor）
   route: game.world.enter
   data : {}
   # => {"world_id":"world"}

   # 查询玩家信息（player actor，按 uid 自动创建）
   route: game.player.getInfo
   data : {}
   # => {"uid":10001}
   ```

   内置演示账号：`admin/admin123`、`test/123456`。

## 关键设计

- **序列化**：全集群统一 `JSON`（`app.SetSerializer(cserializer.NewJSON())`）。
  业务消息是普通 Go 结构体，不需要 protobuf 代码生成；框架内部的跨节点信封仍用
  protobuf 的 `ClusterPacket`，与业务序列化互不干扰。
- **消息路由**：`gate/route.go` 的 `onDataRoute` 是网关的第一道门禁，未登录的连接
  只放行 `gate.user.login`；登录后 `gate.*` 走本节点本地路由，`game.*` 被跨节点转发到
  game 节点的 world / world.<uid>。
- **world / player actor**：`world` 是 game 节点上的父 actor（actorID = "world"），
  `player` 是它的子 actor（childID = uid）。玩家消息被路由到 `world.<uid>`，由
  `ActorWorld.OnFindChild` 按需创建 player actor。两者都嵌入 `pomelo.ActorBase`，
  用 `p.Response(session, v)` 通过 session 里的 AgentPath 把响应回传给网关。
- **远程调用**：`internal/rpc/login` 封装了网关到登录服的 RPC，返回 `(resp, 错误码)`
  的约定与框架一致；错误码非 0 时 `resp` 为 nil。
- **顶号**：`pomelo.Bind(sid, uid)` 返回同一个 uid 之前的旧连接，网关把它踢掉。
  若部署多个网关，还需像框架 `demo_cluster` 那样跨网关广播踢人，本例省略。
