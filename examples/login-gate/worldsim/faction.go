package worldsim

// Faction 阵营：红 / 黑 / 绿。
type Faction int32

const (
	FactionRed   Faction = 0 // 红方
	FactionBlack Faction = 1 // 黑方
	FactionGreen Faction = 2 // 绿方
)

// Relation 两个对象之间的阵营关系。
type Relation int32

const (
	RelFriendly Relation = iota // 友方 -> "hello"
	RelEnemy                    // 敌对 -> "battle"
	RelNeutral                  // 中立 -> "good"
)

// RelationOf 计算 a 与 b 的关系：
//   红红 / 黑黑 -> 友方；红黑 / 黑红 -> 敌对；含绿 -> 中立。
func RelationOf(a, b Faction) Relation {
	if a == FactionGreen || b == FactionGreen {
		return RelNeutral
	}
	if a == b {
		return RelFriendly
	}
	return RelEnemy
}
