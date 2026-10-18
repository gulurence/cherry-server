package worldsim

import "time"

// 动态城堡参数。城堡是不可通行的静态阻挡，客户端预寻路只看静态地形、看不到城堡，
// 因此由服务器在 SetClientPath 里对穿过城堡的路段做绕行修正。
const (
	CastleInterval = 5 * time.Second // 城堡增删周期

	// 城堡按 castleSpan×castleSpan 的槽位放置，每槽内放 castleSize×castleSize 的
	// 城堡并在槽内随机抖动（最多 castleJitter 格）。槽位互不重叠，既保证城堡间绝不
	// 互相压叠、可稳定铺满目标覆盖，又避免整齐网格的观感。
	//
	// 目标覆盖全图 1/8（=125,000 格），单个城堡 64 格，故约需 castleTargetCount 座。
	castleSpan        = 12
	castleSize        = 8
	castleJitter      = castleSpan - castleSize
	castleTargetArea  = MapW * MapH / 8
	castleTargetCount = castleTargetArea / (castleSize * castleSize)

	// castleChurnCount 每个 CastleInterval 周期批量增删的城堡数。之前每周期只增删 1 座，
	// 客户端视野很小（约 1% 地图），几乎看不到删除；批量增删后，视野内每几秒就能看到
	// 一座城堡出现或消失，动态创建/删除对客户端可见。
	castleChurnCount = 100
)

// Castle 一座静态城堡：占据若干连续格子，不可通行；被移除时恢复这些格子。
// 城堡只会放置在“可通行且无对象占据”的格子上，绝不压在场景对象身上。
type Castle struct {
	ID    int
	Cells []Vec2i // 实际被阻挡的格子，删除时据此恢复可通行
	Born  time.Time
}

// buildCastleSlots 生成全图所有城堡槽位的左上角（按 castleSpan 对齐）并随机洗牌，
// 供 AddCastle 顺序扫描放置。
func (g *Game) buildCastleSlots() {
	g.castleSlots = g.castleSlots[:0]
	for y := 0; y+castleSize <= MapH; y += castleSpan {
		for x := 0; x+castleSize <= MapW; x += castleSpan {
			g.castleSlots = append(g.castleSlots, Vec2i{X: x, Y: y})
		}
	}
	g.rng.Shuffle(len(g.castleSlots), func(i, j int) {
		g.castleSlots[i], g.castleSlots[j] = g.castleSlots[j], g.castleSlots[i]
	})
	g.castleSlotIdx = 0
}

// AddCastle 从槽位中找下一个可用位置，放置一座 castleSize×castleSize 的城堡（槽内
// 随机抖动）。位置需整块可通行且不与任何对象（含其周围一圈）重叠。找不到返回 nil。
func (g *Game) AddCastle() *Castle {
	if len(g.castleSlots) == 0 {
		g.buildCastleSlots()
	}

	for i := 0; i < len(g.castleSlots); i++ {
		slot := g.castleSlots[g.castleSlotIdx%len(g.castleSlots)]
		g.castleSlotIdx++
		x := slot.X + g.rng.Intn(castleJitter+1)
		y := slot.Y + g.rng.Intn(castleJitter+1)

		var cells []Vec2i
		ok := true
		for yy := 0; yy < castleSize && ok; yy++ {
			for xx := 0; xx < castleSize && ok; xx++ {
				c := Vec2i{X: x + xx, Y: y + yy}
				if !g.grid.Walkable(c) || g.cellOccupied(c) {
					ok = false
					break
				}
				cells = append(cells, c)
			}
		}
		if !ok {
			continue
		}

		for _, c := range cells {
			g.grid.SetBlocked(c, true)
		}
		castle := &Castle{ID: g.nextCastleID, Cells: cells, Born: time.Now()}
		g.nextCastleID++
		g.castles = append(g.castles, castle)
		return castle
	}
	return nil
}

// SeedCastles 在开局预置城堡，使其覆盖全图约 1/8。记录实际放置数量为 g.castleTarget，
// 供 UpdateCastles 以此维持覆盖并做增删。
func (g *Game) SeedCastles() {
	g.buildCastleSlots()
	for i := 0; i < castleTargetCount; i++ {
		g.AddCastle()
	}
	g.castleTarget = len(g.castles)
	g.refreshSnapshot()
}

// RemoveCastle 移除一座城堡，恢复其阻挡的格子为可通行。
func (g *Game) RemoveCastle(c *Castle) {
	for _, cell := range c.Cells {
		g.grid.SetBlocked(cell, false)
	}
}

// UpdateCastles 按 CastleInterval 周期增删城堡，返回 (新增格, 移除格) 供上层推送。
// 达到 SeedCastles 实际放置的目标数量后，每周期批量移除最旧的 castleChurnCount 座、
// 再补同样数量的新城堡，维持约 1/8 覆盖的同时持续“添加/删除”动态。
func (g *Game) UpdateCastles(now time.Time) (added, removed []Vec2i) {
	if now.Sub(g.lastCastleTick) < CastleInterval {
		return nil, nil
	}
	g.lastCastleTick = now

	if g.castleTarget > 0 && len(g.castles) >= g.castleTarget {
		n := castleChurnCount
		if n > len(g.castles) {
			n = len(g.castles)
		}
		for i := 0; i < n; i++ {
			oldest := g.castles[0]
			g.castles = g.castles[1:]
			removed = append(removed, oldest.Cells...)
			g.RemoveCastle(oldest)
		}
	}

	for i := 0; i < castleChurnCount; i++ {
		if c := g.AddCastle(); c != nil {
			added = append(added, c.Cells...)
		}
	}
	if len(added) > 0 || len(removed) > 0 {
		// 换快照：后续 Game.Update 里检测到“前方 repairPathRange 格内有城堡”触发的
		// RepathAsync 会被 worker 立即取走，须读到含新城堡的快照，否则绕行结果仍会穿过新城堡。
		g.refreshSnapshot()
	}
	return added, removed
}

// AllCastleCells 返回当前所有城堡占据的格子（用于进入世界时的城堡快照）。
func (g *Game) AllCastleCells() []Vec2i {
	var out []Vec2i
	for _, c := range g.castles {
		out = append(out, c.Cells...)
	}
	return out
}

// cellOccupied 判断格子是否落在某个对象的 3×3 邻域内。对象尺寸最大为 3、以
// Cell 为中心绘制，因此 3×3 邻域覆盖了对象的完整占位；城堡绝不压到对象身上。
func (g *Game) cellOccupied(c Vec2i) bool {
	for _, o := range g.objs {
		dx := c.X - o.Cell.X
		dy := c.Y - o.Cell.Y
		if dx >= -1 && dx <= 1 && dy >= -1 && dy <= 1 {
			return true
		}
	}
	return false
}
