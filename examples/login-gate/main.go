// login-gate 示例的统一入口。
//
// 四个进程都从这一个可执行文件启动，靠第一个命令行参数区分：
//
//	master  发现服务，记录"集群里有哪些节点"，不处理业务
//	login   登录服，校验账号密码、签发令牌
//	gate    网关，面向客户端，负责连接管理与登录转发
//	game    游戏服，承载 world / player actor
//
// 启动顺序有讲究：master 必须最先起，login 次之，gate 与 game 可并行，最后客户端接入。
// 其余节点连不上 master 会一直重试，所以顺序错也能跑通，只是会有重试日志。
//
// 本机调试时(在 examples/login-gate 目录下执行)：
//
//	go run . master --path=config/login-gate.json --node=lg-master
//	go run . login  --path=config/login-gate.json --node=lg-login-1
//	go run . gate   --path=config/login-gate.json --node=lg-gate-1
//	go run . game   --path=config/login-gate.json --node=lg-game-1
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gulurence/cherry-server/examples/login-gate/nodes/game"
	"github.com/gulurence/cherry-server/examples/login-gate/nodes/gate"
	"github.com/gulurence/cherry-server/examples/login-gate/nodes/login"
	"github.com/gulurence/cherry-server/examples/login-gate/nodes/master"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	command := os.Args[1]
	path, node := parseFlags(os.Args[2:])

	switch command {
	case "master":
		master.Run(path, node)
	case "login":
		login.Run(path, node)
	case "gate":
		gate.Run(path, node)
	case "game":
		game.Run(path, node)
	default:
		fmt.Printf("unknown command: %s\n\n", command)
		usage()
	}
}

// parseFlags 解析 --path 与 --node 两个参数。
func parseFlags(args []string) (path, node string) {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	fs.StringVar(&path, "path", "config/login-gate.json", "profile 配置文件路径")
	fs.StringVar(&node, "node", "", "本节点 nodeID，必须能在配置文件的 node 段里匹配")
	_ = fs.Parse(args)
	return path, node
}

func usage() {
	fmt.Println("用法: go run . <master|login|gate|game> --path=config/login-gate.json --node=<nodeID>")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  go run . master --node=lg-master")
	fmt.Println("  go run . login  --node=lg-login-1")
	fmt.Println("  go run . gate   --node=lg-gate-1")
	fmt.Println("  go run . game   --node=lg-game-1")
}
