package worldsim

const (
	BucketSize = 8
	BucketW    = MapW / BucketSize
	BucketH    = MapH / BucketSize
)

// Bucket 空间哈希的一个桶。
type Bucket struct {
	objs map[int]*Object
}

// SpatialHash 把对象按所在桶索引，用于加速半径查询。
type SpatialHash struct {
	buckets [BucketW * BucketH]Bucket
}

func NewSpatialHash() *SpatialHash {
	s := &SpatialHash{}
	for i := range s.buckets {
		s.buckets[i].objs = make(map[int]*Object)
	}
	return s
}

func (s *SpatialHash) cellToBucket(c Vec2i) int {
	bx := c.X / BucketSize
	by := c.Y / BucketSize
	if bx < 0 {
		bx = 0
	}
	if bx >= BucketW {
		bx = BucketW - 1
	}
	if by < 0 {
		by = 0
	}
	if by >= BucketH {
		by = BucketH - 1
	}
	return by*BucketW + bx
}

func (s *SpatialHash) Insert(o *Object) {
	s.buckets[s.cellToBucket(o.Cell)].objs[o.ID] = o
}

func (s *SpatialHash) Remove(o *Object) {
	delete(s.buckets[s.cellToBucket(o.Cell)].objs, o.ID)
}

func (s *SpatialHash) Move(o *Object, newCell Vec2i) {
	old := s.cellToBucket(o.Cell)
	neu := s.cellToBucket(newCell)
	if old != neu {
		delete(s.buckets[old].objs, o.ID)
		s.buckets[neu].objs[o.ID] = o
	}
	o.Cell = newCell
}

// QueryCircle 返回中心 p 半径 r 覆盖到的所有对象（桶级粗筛，结果不排序）。
func (s *SpatialHash) QueryCircle(p Vec2, r float32) []*Object {
	minC := Vec2i{int(p.X - r), int(p.Y - r)}
	maxC := Vec2i{int(p.X + r), int(p.Y + r)}

	var result []*Object
	seen := make(map[int]bool)
	minB := s.cellToBucket(minC)
	maxB := s.cellToBucket(maxC)
	minBX := minB % BucketW
	maxBX := maxB % BucketW
	minBY := minB / BucketW
	maxBY := maxB / BucketW

	for by := minBY; by <= maxBY; by++ {
		for bx := minBX; bx <= maxBX; bx++ {
			b := s.buckets[by*BucketW+bx]
			for _, o := range b.objs {
				if seen[o.ID] {
					continue
				}
				seen[o.ID] = true
				result = append(result, o)
			}
		}
	}
	return result
}
