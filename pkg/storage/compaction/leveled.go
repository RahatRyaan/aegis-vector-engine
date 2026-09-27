package compaction

import (
	"bytes"
	"container/heap"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"project-x/pkg/storage/sstable"
)

const (
	MaxLevels       = 7
	L0CompactionTrigger = 4 // Number of L0 files to trigger compaction
	BaseLevelSizeMB = 10 // Level 1 target size
	LevelMultiplier = 10 // Each level is 10x larger
)

type TableMetadata struct {
	ID          uint64
	Level       int
	SmallestKey []byte
	LargestKey  []byte
	Size        uint64
	Path        string
}

type MergeItem struct {
	Entry     sstable.BlockEntry
	SourceIdx int
}

type MergeHeap []MergeItem

func (h MergeHeap) Len() int           { return len(h) }
func (h MergeHeap) Less(i, j int) bool {
	cmp := bytes.Compare(h[i].Entry.Key, h[j].Entry.Key)
	if cmp == 0 {
		// Greater sequence number takes priority
		return h[i].Entry.Seq > h[j].Entry.Seq
	}
	return cmp < 0
}
func (h MergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *MergeHeap) Push(x interface{}) { *h = append(*h, x.(MergeItem)) }
func (h *MergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type CompactionEngine struct {
	dir         string
	levels      [MaxLevels][]*TableMetadata
	mu          sync.RWMutex
	nextTableID uint64
}

func NewCompactionEngine(dir string) *CompactionEngine {
	return &CompactionEngine{
		dir:         dir,
		nextTableID: 1,
	}
}

func (ce *CompactionEngine) NextID() uint64 {
	return atomic.AddUint64(&ce.nextTableID, 1)
}

func (ce *CompactionEngine) RegisterTable(meta *TableMetadata) {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	if meta.Level < MaxLevels {
		ce.levels[meta.Level] = append(ce.levels[meta.Level], meta)
	}
}

func (ce *CompactionEngine) NeedsCompaction() (int, bool) {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	// Check Level 0 file count
	if len(ce.levels[0]) >= L0CompactionTrigger {
		return 0, true
	}

	// Check Levels 1..6 size ratios
	targetSize := uint64(BaseLevelSizeMB * 1024 * 1024)
	for lvl := 1; lvl < MaxLevels-1; lvl++ {
		totalSize := uint64(0)
		for _, tbl := range ce.levels[lvl] {
			totalSize += tbl.Size
		}
		if totalSize > targetSize {
			return lvl, true
		}
		targetSize *= LevelMultiplier
	}

	return 0, false
}

func (ce *CompactionEngine) CompactLevel(level int) error {
	ce.mu.Lock()
	if level >= MaxLevels-1 || len(ce.levels[level]) == 0 {
		ce.mu.Unlock()
		return nil
	}

	sourceTables := ce.levels[level]
	targetLevel := level + 1
	var targetTables []*TableMetadata

	// Find overlapping tables in targetLevel
	var smallestKey, largestKey []byte
	for _, src := range sourceTables {
		if len(smallestKey) == 0 || bytes.Compare(src.SmallestKey, smallestKey) < 0 {
			smallestKey = src.SmallestKey
		}
		if len(largestKey) == 0 || bytes.Compare(src.LargestKey, largestKey) > 0 {
			largestKey = src.LargestKey
		}
	}

	for _, tgt := range ce.levels[targetLevel] {
		if bytes.Compare(tgt.LargestKey, smallestKey) >= 0 && bytes.Compare(tgt.SmallestKey, largestKey) <= 0 {
			targetTables = append(targetTables, tgt)
		}
	}

	allInputs := append([]*TableMetadata{}, sourceTables...)
	allInputs = append(allInputs, targetTables...)
	ce.mu.Unlock()

	// Multi-way merge of all entries
	var iterators [][]sstable.BlockEntry
	for _, meta := range allInputs {
		f, err := os.Open(meta.Path)
		if err != nil {
			return err
		}
		tr, err := sstable.OpenTableReader(f)
		if err != nil {
			_ = f.Close()
			return err
		}
		entries, err := tr.AllEntries()
		_ = tr.Close()
		if err != nil {
			return err
		}
		iterators = append(iterators, entries)
	}

	h := &MergeHeap{}
	heap.Init(h)
	indices := make([]int, len(iterators))

	for i, it := range iterators {
		if len(it) > 0 {
			heap.Push(h, MergeItem{Entry: it[0], SourceIdx: i})
			indices[i] = 1
		}
	}

	newTableID := ce.NextID()
	newPath := filepath.Join(ce.dir, fmt.Sprintf("%06d.sst", newTableID))
	newFile, err := os.OpenFile(newPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	tb := sstable.NewTableBuilder(newFile, 10000)
	var lastKey []byte

	for h.Len() > 0 {
		top := heap.Pop(h).(MergeItem)
		srcIdx := top.SourceIdx

		// Advance source iterator
		if indices[srcIdx] < len(iterators[srcIdx]) {
			heap.Push(h, MergeItem{
				Entry:     iterators[srcIdx][indices[srcIdx]],
				SourceIdx: srcIdx,
			})
			indices[srcIdx]++
		}

		// Deduplicate: only take highest sequence number per key
		if bytes.Equal(top.Entry.Key, lastKey) {
			continue
		}

		lastKey = append(lastKey[:0], top.Entry.Key...)

		// Drop delete tombstones if compacting into deepest level
		if top.Entry.Op == 2 && targetLevel == MaxLevels-1 {
			continue
		}

		if err := tb.Add(top.Entry.Key, top.Entry.Value, top.Entry.Seq, top.Entry.Op); err != nil {
			_ = newFile.Close()
			return err
		}
	}

	if err := tb.Finish(); err != nil {
		_ = newFile.Close()
		return err
	}
	_ = newFile.Close()

	stat, _ := os.Stat(newPath)
	newMeta := &TableMetadata{
		ID:          newTableID,
		Level:       targetLevel,
		SmallestKey: tb.SmallestKey(),
		LargestKey:  tb.LargestKey(),
		Size:        uint64(stat.Size()),
		Path:        newPath,
	}

	// Atomically swap metadata in levels
	ce.mu.Lock()
	defer ce.mu.Unlock()

	// Clear source level
	ce.levels[level] = nil

	// Remove compacted target tables
	var remainingTarget []*TableMetadata
	inputMap := make(map[uint64]bool)
	for _, in := range targetTables {
		inputMap[in.ID] = true
	}
	for _, tgt := range ce.levels[targetLevel] {
		if !inputMap[tgt.ID] {
			remainingTarget = append(remainingTarget, tgt)
		}
	}

	remainingTarget = append(remainingTarget, newMeta)
	sort.Slice(remainingTarget, func(i, j int) bool {
		return bytes.Compare(remainingTarget[i].SmallestKey, remainingTarget[j].SmallestKey) < 0
	})
	ce.levels[targetLevel] = remainingTarget

	// Clean up old files on disk
	for _, old := range allInputs {
		_ = os.Remove(old.Path)
	}

	return nil
}
