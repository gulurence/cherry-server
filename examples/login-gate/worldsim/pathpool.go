package worldsim

import (
	"sync"
	"sync/atomic"
	"time"
)

// PathPool 是一个固定大小的寻路 worker 池，把 CPU 密集的 A*（玩家 moveTo 的全图寻路）
// 从 world actor goroutine 拆出来并行执行。
//
// 并发模型：
//   - 每个 worker 持独立 *Pathfinder（复用其平铺 scratch，非并发安全，故一 worker 一份）。
//   - worker 只读一份不可变的 walkable 快照（atomic.Pointer[[]bool]）。城堡增删发生在
//     world actor goroutine，增删完成后由上层调用 Refresh 整体换新快照，worker 全程只读、
//     无需加锁。
//   - Submit 由 world actor 串行调用（入队不阻塞 A*）；结果写回 resultCh，由 world actor
//     在 tick 里通过 Drain 非阻塞取走回填（对象路径赋值 + 推送 + 响应仍留在 actor goroutine）。
type PathPool struct {
	workers  int
	jobCh    chan PathJob
	resultCh chan PathResult
	snapshot atomic.Pointer[[]bool]
	wg       sync.WaitGroup
}

// PathJob.Kind 区分寻路用途：
//   PathMoveTo —— 玩家 moveTo，结果由 actor 通过 pendingMove 回填并响应会话；
//   PathRepath —— 城堡增删后的反向修正重寻路，结果由 Game.ApplyRepathResult 回填对象。
const (
	PathMoveTo = iota
	PathRepath
)

// PathJob 是一次寻路请求。Token 由调用方指定（如 uid 或自增 id），原样带回结果以便关联。
type PathJob struct {
	Token     int64
	Start     Vec2i
	Goal      Vec2i
	MaxExpand int
	Kind      int
}

// PathResult 是一次寻路结果。Path 为 nil 表示不可达（或超展开上限）。
type PathResult struct {
	Token  int64
	Path   []Vec2i
	CostMs int64
	Kind   int
}

// 通道缓冲足够容纳 1000 客户端同时 moveTo 的入队（约 50ms 内全部入队），避免 world actor
// 在 Submit 时因 worker 消费不及而被阻塞——一旦 actor 阻塞就又退化成串行。
const (
	pathJobBuf    = 4096
	pathResultBuf = 2048
)

// NewPathPool 创建 worker 池并启动 workers 个后台 goroutine。workers<=0 时取 1。
func NewPathPool(workers int) *PathPool {
	if workers <= 0 {
		workers = 1
	}
	p := &PathPool{
		workers:  workers,
		jobCh:    make(chan PathJob, pathJobBuf),
		resultCh: make(chan PathResult, pathResultBuf),
	}
	p.snapshot.Store(&[]bool{})
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.workerLoop(NewPathfinder())
	}
	return p
}

// workerLoop 是单个 worker 的主循环：取任务 -> 在快照上跑 A* -> 写结果。
func (p *PathPool) workerLoop(pf *Pathfinder) {
	defer p.wg.Done()
	for job := range p.jobCh {
		start := time.Now()
		snap := *p.snapshot.Load()
		var path []Vec2i
		if len(snap) == MapW*MapH {
			// 均匀代价网格下用 JPS（语义与 A* 一致，已验证最优性与合法性），跳过大量中间节点。
			path = pf.FindJPS(snap, job.Start, job.Goal, job.MaxExpand)
		} else {
			// 快照尚未初始化（极端：首次请求早于 Refresh），直接判不可达。
			path = nil
		}
		p.resultCh <- PathResult{Token: job.Token, Path: path, CostMs: time.Since(start).Milliseconds(), Kind: job.Kind}
	}
}

// Refresh 用当前 grid 的可通行数组整体换新快照（拷贝一份，之后 worker 只读该副本）。
// 由 world actor 在城堡增删（SeedCastles / UpdateCastles）后串行调用。
func (p *PathPool) Refresh(walkable []bool) {
	cp := make([]bool, len(walkable))
	copy(cp, walkable)
	p.snapshot.Store(&cp)
}

// Submit 入队一次寻路。world actor 串行调用；缓冲满时会阻塞（正常压测规模下不会发生）。
func (p *PathPool) Submit(job PathJob) {
	p.jobCh <- job
}

// Drain 非阻塞取走当前已就绪的所有结果并逐一回调。由 world actor 在 tick 里调用。
func (p *PathPool) Drain(fn func(PathResult)) {
	for {
		select {
		case r := <-p.resultCh:
			fn(r)
		default:
			return
		}
	}
}
