package worldsim

// Jump Point Search（JPS）：在均匀代价网格（Grid.cost 恒为 1）上跳过 A* 展开的大量中间
// 节点，只展开"跳点"。与 FindWalkable 语义一致：8 方向、斜向不穿墙角、Octile 启发，
// 返回逐格路径（相邻跳点之间用直线插值补齐）。均匀网格下比 A* 快数倍到数十倍。

// signInt 返回 x 的符号（-1/0/1）。
func signInt(x int) int {
	if x > 0 {
		return 1
	}
	if x < 0 {
		return -1
	}
	return 0
}

// jpsJump 从 (x,y) 沿方向 (dx,dy) 一路跳，返回发现的跳点格子下标；无跳点返回 -1。
// 终点 (gx,gy) 也算跳点。
func jpsJump(walkable []bool, x, y, dx, dy, gx, gy int) int {
	nx, ny := x+dx, y+dy
	if nx < 0 || nx >= MapW || ny < 0 || ny >= MapH {
		return -1
	}
	if !walkable[ny*MapW+nx] {
		return -1
	}
	// 斜向不穿墙角：移到 (nx,ny) 前先验证两个正交邻居都可走。
	// 必须在目标判定之前，否则终点也会被斜向穿墙直达。
	if dx != 0 && dy != 0 && (!walkable[ny*MapW+x] || !walkable[y*MapW+nx]) {
		return -1
	}
	if nx == gx && ny == gy {
		return ny*MapW + nx
	}

	if dx != 0 && dy != 0 {
		// 斜向的两个正交分量方向若存在跳点，则本格就是跳点。
		if jpsJump(walkable, nx, ny, dx, 0, gx, gy) != -1 {
			return ny*MapW + nx
		}
		if jpsJump(walkable, nx, ny, 0, dy, gx, gy) != -1 {
			return ny*MapW + nx
		}
	} else if dx != 0 {
		// 水平：后侧对角被挡（墙角）时，本格为跳点（需垂直/对角绕行）。
		if ny+1 < MapH && !walkable[(ny+1)*MapW+(nx-dx)] {
			return ny*MapW + nx
		}
		if ny-1 >= 0 && !walkable[(ny-1)*MapW+(nx-dx)] {
			return ny*MapW + nx
		}
	} else {
		// 垂直：后侧对角被挡时，本格为跳点。
		if nx+1 < MapW && !walkable[(ny-dy)*MapW+(nx+1)] {
			return ny*MapW + nx
		}
		if nx-1 >= 0 && !walkable[(ny-dy)*MapW+(nx-1)] {
			return ny*MapW + nx
		}
	}

	return jpsJump(walkable, nx, ny, dx, dy, gx, gy)
}

// jpsSuccessors 返回从节点 (cx,cy)（父方向 pdx,pdy；起点 pdx==0&&pdy==0）出发应尝试的跳点方向。
// 强制邻居方向与 jpsJump 内的检测条件保持一致：直向父要额外给出垂直两侧的强制邻居方向。
func jpsSuccessors(walkable []bool, cx, cy, pdx, pdy int) [][2]int {
	if pdx == 0 && pdy == 0 {
		return [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	}
	if pdx != 0 && pdy != 0 {
		// 斜向父：继续斜向 + 两个正交分量（强制邻居由 jump 的正交分量递归检测）。
		dirs := [][2]int{{pdx, pdy}}
		if cx+pdx >= 0 && cx+pdx < MapW && walkable[cy*MapW+(cx+pdx)] {
			dirs = append(dirs, [2]int{pdx, 0})
		}
		if cy+pdy >= 0 && cy+pdy < MapH && walkable[(cy+pdy)*MapW+cx] {
			dirs = append(dirs, [2]int{0, pdy})
		}
		return dirs
	}
	if pdx != 0 {
		// 水平父：继续直行 + 后侧对角被挡时的垂直/对角强制方向。
		dirs := [][2]int{{pdx, 0}}
		if cy+1 < MapH && !walkable[(cy+1)*MapW+(cx-pdx)] {
			dirs = append(dirs, [2]int{0, 1}, [2]int{pdx, 1})
		}
		if cy-1 >= 0 && !walkable[(cy-1)*MapW+(cx-pdx)] {
			dirs = append(dirs, [2]int{0, -1}, [2]int{pdx, -1})
		}
		return dirs
	}
	// 垂直父：继续直行 + 后侧对角被挡时的水平/对角强制方向。
	dirs := [][2]int{{0, pdy}}
	if cx+1 < MapW && !walkable[(cy-pdy)*MapW+(cx+1)] {
		dirs = append(dirs, [2]int{1, 0}, [2]int{1, pdy})
	}
	if cx-1 >= 0 && !walkable[(cy-pdy)*MapW+(cx-1)] {
		dirs = append(dirs, [2]int{-1, 0}, [2]int{-1, pdy})
	}
	return dirs
}

// FindJPS 在 walkable 快照上求 start 到 goal 的路径（JPS 版）。
// maxExpand 语义同 FindWalkable（0 表示不设上限）。
func (pf *Pathfinder) FindJPS(walkable []bool, start, goal Vec2i, maxExpand int) []Vec2i {
	if start.X < 0 || start.Y < 0 || start.X >= MapW || start.Y >= MapH ||
		!walkable[start.Y*MapW+start.X] {
		return nil
	}
	if goal.X < 0 || goal.Y < 0 || goal.X >= MapW || goal.Y >= MapH ||
		!walkable[goal.Y*MapW+goal.X] {
		return nil
	}
	if start == goal {
		return []Vec2i{start}
	}

	pf.gen++
	gen := pf.gen
	pf.openF = pf.openF[:0]
	pf.openCell = pf.openCell[:0]

	si := start.Y*MapW + start.X
	gi := goal.Y*MapW + goal.X
	gx, gy := goal.X, goal.Y
	pf.gScore[si] = 0
	pf.gGen[si] = gen
	pf.parent[si] = int32(si)
	pf.heapPush(octileH(start.X, start.Y, gx, gy), int32(si))

	expanded := 0
	for len(pf.openF) > 0 {
		ci := int(pf.heapPop())
		if pf.closed[ci] == gen {
			continue
		}
		pf.closed[ci] = gen
		expanded++
		if maxExpand > 0 && expanded > maxExpand {
			return nil
		}
		if ci == gi {
			return pf.reconstructJPS(si, gi)
		}

		cx, cy := ci%MapW, ci/MapW
		baseG := pf.gScore[ci]
		pi := int(pf.parent[ci])
		pdx, pdy := 0, 0
		if pi != ci {
			pdx = signInt(cx - pi%MapW)
			pdy = signInt(cy - pi/MapW)
		}
		for _, d := range jpsSuccessors(walkable, cx, cy, pdx, pdy) {
			ji := jpsJump(walkable, cx, cy, d[0], d[1], gx, gy)
			if ji < 0 {
				continue
			}
			if pf.closed[ji] == gen {
				continue
			}
			jx, jy := ji%MapW, ji/MapW
			ng := baseG + octileH(cx, cy, jx, jy)
			if pf.gGen[ji] != gen || ng < pf.gScore[ji] {
				pf.gGen[ji] = gen
				pf.gScore[ji] = ng
				pf.parent[ji] = int32(ci)
				pf.heapPush(ng+octileH(jx, jy, gx, gy), int32(ji))
			}
		}
	}
	return nil
}

// reconstructJPS 从 goal 沿 parent 链回溯到 start，并在相邻跳点间做直线插值。
func (pf *Pathfinder) reconstructJPS(startIdx, goalIdx int) []Vec2i {
	var pts []int
	for i := goalIdx; ; i = int(pf.parent[i]) {
		pts = append(pts, i)
		if i == startIdx {
			break
		}
	}

	path := make([]Vec2i, 0, 1024)
	cx, cy := startIdx%MapW, startIdx/MapW
	for k := len(pts) - 1; k >= 1; k-- {
		t := pts[k-1]
		tx, ty := t%MapW, t/MapW
		dx := signInt(tx - cx)
		dy := signInt(ty - cy)
		for cx != tx || cy != ty {
			path = append(path, Vec2i{cx, cy})
			cx += dx
			cy += dy
		}
	}
	path = append(path, Vec2i{cx, cy})
	return path
}
