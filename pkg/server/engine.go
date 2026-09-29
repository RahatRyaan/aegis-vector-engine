package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"project-x/pkg/ai"
	"project-x/pkg/consensus/multiraft"
	partition "project-x/pkg/consensus/range"
	"project-x/pkg/pd"
	"project-x/pkg/search"
	"project-x/pkg/storage/compaction"
	"project-x/pkg/storage/memtable"
	"project-x/pkg/storage/sstable"
	"project-x/pkg/storage/wal"
	"project-x/pkg/vector/distance"
	"project-x/pkg/vector/hnsw"
)

type Config struct {
	DataDir        string
	NodeID         uint64
	VectorDim      int
	VectorMetric   distance.MetricType
	MemTableSize   uint64
	WALBatchWindow time.Duration
}

type Engine struct {
	cfg         Config
	memTable    *memtable.ConcurrentSkiplist
	immMem      *memtable.ConcurrentSkiplist
	wal         *wal.WAL
	compactor   *compaction.CompactionEngine
	vectorIdx   *hnsw.HNSWIndex
	bm25Idx     *search.BM25Index
	hybrid      *search.HybridSearchEngine
	embedder    *ai.EmbeddingGenerator
	memoryStore *ai.MemoryStore
	agent       *ai.AIAgent
	router      *partition.RangeRouter
	pdClient    *pd.PlacementDriver
	multiRaft   *multiraft.MultiRaftEngine

	mu         sync.RWMutex
	seq        uint64
	closed     uint32
	tables     []*sstable.TableReader
	activeFlsh sync.Mutex
}

func OpenEngine(cfg Config) (*Engine, error) {
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}

	if cfg.MemTableSize == 0 {
		cfg.MemTableSize = 64 * 1024 * 1024
	}
	if cfg.VectorDim == 0 {
		cfg.VectorDim = 128
	}

	walDir := filepath.Join(cfg.DataDir, "wal")
	w, err := wal.OpenWAL(walDir, cfg.WALBatchWindow)
	if err != nil {
		return nil, err
	}

	arena := memtable.NewArena(cfg.MemTableSize)
	mem := memtable.NewConcurrentSkiplist(arena)

	router := partition.NewRangeRouter()
	pdInst := pd.NewPlacementDriver(router)
	mRaft := multiraft.NewMultiRaftEngine(cfg.NodeID, nil, router)

	vIdx := hnsw.NewHNSWIndex(cfg.VectorDim, cfg.VectorMetric)
	bm := search.NewBM25Index()
	hybn := search.NewHybridSearchEngine(bm, vIdx, 0.5)
	emb := ai.NewEmbeddingGenerator(cfg.VectorDim)

	eng := &Engine{
		cfg:       cfg,
		memTable:  mem,
		wal:       w,
		compactor: compaction.NewCompactionEngine(cfg.DataDir),
		vectorIdx: vIdx,
		bm25Idx:   bm,
		hybrid:    hybn,
		embedder:  emb,
		router:    router,
		pdClient:  pdInst,
		multiRaft: mRaft,
	}

	memStore := ai.NewMemoryStore(eng, emb)
	eng.memoryStore = memStore
	eng.agent = ai.NewAIAgent(eng, memStore)

	maxSeq, err := w.Recover(func(rec *wal.Record) error {
		return mem.Put(rec.Key, rec.Value, rec.Type, rec.Seq)
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	eng.seq = maxSeq

	files, _ := filepath.Glob(filepath.Join(cfg.DataDir, "*.sst"))
	for _, fPath := range files {
		f, err := os.Open(fPath)
		if err != nil {
			continue
		}
		tr, err := sstable.OpenTableReader(f)
		if err == nil {
			eng.tables = append(eng.tables, tr)
		}
	}

	return eng, nil
}

func (e *Engine) Put(key, value []byte) error {
	if atomic.LoadUint32(&e.closed) == 1 {
		return os.ErrClosed
	}
	seq := atomic.AddUint64(&e.seq, 1)

	if err := e.wal.Append(seq, memtable.OpPut, key, value); err != nil {
		return err
	}
	if err := e.memTable.Put(key, value, memtable.OpPut, seq); err != nil {
		if errors.Is(err, memtable.ErrArenaFull) {
			if err := e.rotateAndFlushMemtable(); err != nil {
				return err
			}
			return e.memTable.Put(key, value, memtable.OpPut, seq)
		}
		return err
	}
	return nil
}

func (e *Engine) Delete(key []byte) error {
	if atomic.LoadUint32(&e.closed) == 1 {
		return os.ErrClosed
	}
	seq := atomic.AddUint64(&e.seq, 1)
	if err := e.wal.Append(seq, memtable.OpDel, key, nil); err != nil {
		return err
	}
	return e.memTable.Put(key, nil, memtable.OpDel, seq)
}

func (e *Engine) Get(key []byte) ([]byte, bool, error) {
	val, _, op, found := e.memTable.Get(key)
	if found {
		if op == memtable.OpDel {
			return nil, false, nil
		}
		return val, true, nil
	}

	e.mu.RLock()
	if e.immMem != nil {
		val, _, op, found := e.immMem.Get(key)
		if found {
			e.mu.RUnlock()
			if op == memtable.OpDel {
				return nil, false, nil
			}
			return val, true, nil
		}
	}

	for i := len(e.tables) - 1; i >= 0; i-- {
		tr := e.tables[i]
		val, _, op, found, err := tr.Get(key)
		if err != nil {
			e.mu.RUnlock()
			return nil, false, err
		}
		if found {
			e.mu.RUnlock()
			if op == memtable.OpDel {
				return nil, false, nil
			}
			return val, true, nil
		}
	}
		
	e.mu.RUnlock()
	return nil, false, nil
}

func (e *Engine) rotateAndFlushMemtable() error {
	e.activeFlsh.Lock()
	defer e.activeFlsh.Unlock()

	e.mu.Lock()
	e.immMem = e.memTable
	newArena := memtable.NewArena(e.cfg.MemTableSize)
	e.memTable = memtable.NewConcurrentSkiplist(newArena)
	e.mu.Unlock()

	tableID := e.compactor.NextID()
	sstPath := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", tableID))
	file, err := os.OpenFile(sstPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	tb := sstable.NewTableBuilder(file, int(e.immMem.Count()))
	it := e.immMem.NewIterator()
	it.SeekToFirst()
	for it.Valid() {
		_ = tb.Add(it.Key(), it.Value(), it.Seq(), it.OpType())
		it.Next()
	}
	if err := tb.Finish(); err != nil {
		_ = file.Close()
		return err
	}
	_ = file.Close()

	f, err := os.Open(sstPath)
	if err == nil {
		tr, err := sstable.OpenTableReader(f)
		if err == nil {
			e.mu.Lock()
			e.tables = append(e.tables, tr)
			e.immMem = nil
			e.mu.Unlock()
		}
	}

	return nil
}

func (e *Engine) InsertVector(id uint64, vec []float32) {
	e.vectorIdx.Insert(id, vec)
}

func (e *Engine) SearchVector(query []float32, k int) []hnsw.DistItem {
	return e.vectorIdx.Search(query, k)
}

func (e *Engine) HybridSearch(queryText string, queryVec []float32, k int) []search.HybridResult {
	if len(queryVec) == 0 {
		queryVec = e.embedder.GenerateEmbedding(queryText)
	}
	return e.hybrid.HybridSearch(queryText, queryVec, k)
}

func (e *Engine) IndexDocument(id uint64, text string) {
	vec := e.embedder.GenerateEmbedding(text)
	e.vectorIdx.Insert(id, vec)
	e.bm25Idx.IndexDocument(id, text)
}

func (e *Engine) Agent() *ai.AIAgent { return e.agent }
func (e *Engine) MemoryStore() *ai.MemoryStore { return e.memoryStore }
func (e *Engine) VectorCount() uint64 { return e.vectorIdx.Count() }

func (e *Engine) Close() error {
	if !atomic.CompareAndSwapUint32(&e.closed, 0, 1) {
		return nil
	}
	e.multiRaft.StopAll()
	if err := e.wal.Close(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tr := range e.tables {
		_ = tr.Close()
	}
	return nil
}
