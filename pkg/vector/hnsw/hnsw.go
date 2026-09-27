package hnsw

import (
	"container/heap"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"

	"project-x/pkg/vector/distance"
)

const (
	DefaultM              = 16
	DefaultM0             = 32
	DefaultEfConstruction = 128
	DefaultEfSearch       = 64
)

type Node struct {
	ID        uint64
	Vector    []float32
	MaxLayer  int
	Neighbors [][]uint64 // Neighbors[layer] = []nodeID
	mu        sync.RWMutex
}

type DistItem struct {
	ID   uint64
	Dist float32
}

type MinHeap []DistItem
func (h MinHeap) Len() int           { return len(h) }
func (h MinHeap) Less(i, j int) bool { return h[i].Dist < h[j].Dist }
func (h MinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *MinHeap) Push(x interface{}) { *h = append(*h, x.(DistItem)) }
func (h *MinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type MaxHeap []DistItem
func (h MaxHeap) Len() int           { return len(h) }
func (h MaxHeap) Less(i, j int) bool { return h[i].Dist > h[j].Dist }
func (h MaxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *MaxHeap) Push(x interface{}) { *h = append(*h, x.(DistItem)) }
func (h *MaxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type HNSWIndex struct {
	Dim            int
	M              int
	M0             int
	EfConstruction int
	EfSearch       int
	Metric         distance.MetricType
	mL             float64

	nodes      sync.Map // map[uint64]*Node
	enterPoint *Node
	maxLayer   int32
	mu         sync.RWMutex
	count      uint64
}

func NewHNSWIndex(dim int, metric distance.MetricType) *HNSWIndex {
	m := DefaultM
	return &HNSWIndex{
		Dim:            dim,
		M:              m,
		M0:             DefaultM0,
		EfConstruction: DefaultEfConstruction,
		EfSearch:       DefaultEfSearch,
		Metric:         metric,
		mL:             1.0 / math.Log(float64(m)),
		maxLayer:       -1,
	}
}

func (idx *HNSWIndex) computeDist(a, b []float32) float32 {
	switch idx.Metric {
	case distance.Cosine:
		return distance.CosineDistance(a, b)
	case distance.DotProduct:
		return distance.DotProductDistance(a, b)
	default:
		return distance.L2Distance(a, b)
	}
}

func (idx *HNSWIndex) randomLayer() int {
	r := rand.Float64()
	if r == 0 {
		r = 0.0000001
	}
	lvl := int(-math.Log(r) * idx.mL)
	if lvl > 16 {
		lvl = 16
	}
	return lvl
}

func (idx *HNSWIndex) Insert(id uint64, vec []float32) {
	nodeLevel := idx.randomLayer()

	newNode := &Node{
		ID:        id,
		Vector:    vec,
		MaxLayer:  nodeLevel,
		Neighbors: make([][]uint64, nodeLevel+1),
	}
	for l := 0; l <= nodeLevel; l++ {
		newNode.Neighbors[l] = make([]uint64, 0, idx.M0)
	}

	idx.nodes.Store(id, newNode)
	atomic.AddUint64(&idx.count, 1)

	idx.mu.Lock()
	currEP := idx.enterPoint
	currMaxLvl := int(atomic.LoadInt32(&idx.maxLayer))

	if currEP == nil {
		idx.enterPoint = newNode
		atomic.StoreInt32(&idx.maxLayer, int32(nodeLevel))
		idx.mu.Unlock()
		return
	}
	idx.mu.Unlock()

	ep := currEP
	currDist := idx.computeDist(vec, ep.Vector)

	// 1. Top down to nodeLevel + 1: greedy 1-NN
	for l := currMaxLvl; l > nodeLevel; l-- {
		changed := true
		for changed {
			changed = false
			ep.mu.RLock()
			var neighbors []uint64
			if l < len(ep.Neighbors) {
				neighbors = append([]uint64(nil), ep.Neighbors[l]...)
			}
			ep.mu.RUnlock()

			for _, nID := range neighbors {
				val, ok := idx.nodes.Load(nID)
				if !ok {
					continue
				}
				neighborNode := val.(*Node)
				d := idx.computeDist(vec, neighborNode.Vector)
				if d < currDist {
					currDist = d
					ep = neighborNode
					changed = true
				}
			}
		}
	}

	// 2. From nodeLevel down to 0: search efConstruction nearest and connect
	for l := min(nodeLevel, currMaxLvl); l >= 0; l-- {
		candidates := idx.searchLayer(vec, ep, idx.EfConstruction, l)
		mMax := idx.M
		if l == 0 {
			mMax = idx.M0
		}

		selected := idx.selectNeighbors(candidates, mMax)
		newNode.Neighbors[l] = selected

		// Bidirectional link
		for _, nID := range selected {
			val, ok := idx.nodes.Load(nID)
			if !ok {
				continue
			}
			neighbor := val.(*Node)
			neighbor.mu.Lock()
			if l < len(neighbor.Neighbors) {
				neighbor.Neighbors[l] = append(neighbor.Neighbors[l], newNode.ID)
				if len(neighbor.Neighbors[l]) > mMax {
					// Shrink neighbor list
					neighbor.Neighbors[l] = idx.shrinkNeighbors(neighbor.Vector, neighbor.Neighbors[l], mMax)
				}
			}
			neighbor.mu.Unlock()
		}

		if len(candidates) > 0 {
			val, ok := idx.nodes.Load(candidates[0].ID)
			if ok {
				ep = val.(*Node)
			}
		}
	}

	idx.mu.Lock()
	if nodeLevel > int(atomic.LoadInt32(&idx.maxLayer)) {
		idx.enterPoint = newNode
		atomic.StoreInt32(&idx.maxLayer, int32(nodeLevel))
	}
	idx.mu.Unlock()
}

func (idx *HNSWIndex) searchLayer(query []float32, ep *Node, ef int, layer int) []DistItem {
	visited := make(map[uint64]bool)
	visited[ep.ID] = true

	candidates := &MinHeap{}
	results := &MaxHeap{}
	heap.Init(candidates)
	heap.Init(results)

	d := idx.computeDist(query, ep.Vector)
	heap.Push(candidates, DistItem{ID: ep.ID, Dist: d})
	heap.Push(results, DistItem{ID: ep.ID, Dist: d})

	for candidates.Len() > 0 {
		c := heap.Pop(candidates).(DistItem)
		furthest := (*results)[0]

		if c.Dist > furthest.Dist {
			break
		}

		val, ok := idx.nodes.Load(c.ID)
		if !ok {
			continue
		}
		cNode := val.(*Node)

		cNode.mu.RLock()
		var neighbors []uint64
		if layer < len(cNode.Neighbors) {
			neighbors = append([]uint64(nil), cNode.Neighbors[layer]...)
		}
		cNode.mu.RUnlock()

		for _, nID := range neighbors {
			if !visited[nID] {
				visited[nID] = true
				nVal, nOk := idx.nodes.Load(nID)
				if !nOk {
					continue
				}
				nNode := nVal.(*Node)
				nDist := idx.computeDist(query, nNode.Vector)

				if results.Len() < ef || nDist < (*results)[0].Dist {
					heap.Push(candidates, DistItem{ID: nID, Dist: nDist})
					heap.Push(results, DistItem{ID: nID, Dist: nDist})
					if results.Len() > ef {
						heap.Pop(results)
					}
				}
			}
		}
	}

	out := make([]DistItem, results.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(results).(DistItem)
	}
	return out
}

func (idx *HNSWIndex) selectNeighbors(candidates []DistItem, m int) []uint64 {
	out := make([]uint64, 0, m)
	for i := 0; i < len(candidates) && len(out) < m; i++ {
		out = append(out, candidates[i].ID)
	}
	return out
}

func (idx *HNSWIndex) shrinkNeighbors(baseVec []float32, neighborIDs []uint64, mMax int) []uint64 {
	var items []DistItem
	for _, id := range neighborIDs {
		val, ok := idx.nodes.Load(id)
		if ok {
			d := idx.computeDist(baseVec, val.(*Node).Vector)
			items = append(items, DistItem{ID: id, Dist: d})
		}
	}

	h := &MinHeap{}
	heap.Init(h)
	for _, it := range items {
		heap.Push(h, it)
	}

	out := make([]uint64, 0, mMax)
	for h.Len() > 0 && len(out) < mMax {
		out = append(out, heap.Pop(h).(DistItem).ID)
	}
	return out
}

func (idx *HNSWIndex) Search(query []float32, k int) []DistItem {
	idx.mu.RLock()
	ep := idx.enterPoint
	maxLvl := int(atomic.LoadInt32(&idx.maxLayer))
	idx.mu.RUnlock()

	if ep == nil || k <= 0 {
		return nil
	}

	currDist := idx.computeDist(query, ep.Vector)

	// Top layers 1-NN greedy descent
	for l := maxLvl; l > 0; l-- {
		changed := true
		for changed {
			changed = false
			ep.mu.RLock()
			var neighbors []uint64
			if l < len(ep.Neighbors) {
				neighbors = append([]uint64(nil), ep.Neighbors[l]...)
			}
			ep.mu.RUnlock()

			for _, nID := range neighbors {
				val, ok := idx.nodes.Load(nID)
				if !ok {
					continue
				}
				nNode := val.(*Node)
				d := idx.computeDist(query, nNode.Vector)
				if d < currDist {
					currDist = d
					ep = nNode
					changed = true
				}
			}
		}
	}

	// Layer 0 beam search with efSearch
	candidates := idx.searchLayer(query, ep, max(idx.EfSearch, k), 0)
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates
}

func (idx *HNSWIndex) Count() uint64 {
	return atomic.LoadUint64(&idx.count)
}
