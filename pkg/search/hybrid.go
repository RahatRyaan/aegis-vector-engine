package search

import (
	"sort"

	"project-x/pkg/vector/hnsw"
)

const (
	RRFConstantK = 60.0
)

type HybridResult struct {
	DocID       uint64  `json:"doc_id"`
	RRFScore    float64 `json:"rrf_score"`
	DenseRank   int     `json:"dense_rank"`
	SparseRank  int     `json:"sparse_rank"`
	DenseDist   float32 `json:"dense_dist"`
	SparseScore float64 `json:"sparse_score"`
	Text        string  `json:"text,omitempty"`
}

type HybridSearchEngine struct {
	bm25   *BM25Index
	hnsw   *hnsw.HNSWIndex
	alpha  float64 // Weight for dense results (0.0 to 1.0, default 0.5)
}

func NewHybridSearchEngine(bm25 *BM25Index, hnsw *hnsw.HNSWIndex, alpha float64) *HybridSearchEngine {
	if alpha <= 0 || alpha > 1.0 {
		alpha = 0.5
	}
	return &HybridSearchEngine{
		bm25:  bm25,
		hnsw:  hnsw,
		alpha: alpha,
	}
}

func (hse *HybridSearchEngine) HybridSearch(queryText string, queryVec []float32, topK int) []HybridResult {
	if topK <= 0 {
		topK = 10
	}

	denseResults := hse.hnsw.Search(queryVec, topK*2)
	sparseResults := hse.bm25.Search(queryText, topK*2)

	type scoreHolder struct {
		denseRank   int
		sparseRank  int
		denseDist   float32
		sparseScore float64
		rrfScore    float64
	}

	merged := make(map[uint64]*scoreHolder)

	// Process dense ranking
	for rank, item := range denseResults {
		h, exists := merged[item.ID]
		if !exists {
			h = &scoreHolder{denseRank: -1, sparseRank: -1}
			merged[item.ID] = h
		}
		h.denseRank = rank + 1
		h.denseDist = item.Dist
		h.rrfScore += hse.alpha / (RRFConstantK + float64(rank+1))
	}

	// Process sparse ranking
	for rank, item := range sparseResults {
		h, exists := merged[item.DocID]
		if !exists {
			h = &scoreHolder{denseRank: -1, sparseRank: -1}
			merged[item.DocID] = h
		}
		h.sparseRank = rank + 1
		h.sparseScore = item.Score
		h.rrfScore += (1.0 - hse.alpha) / (RRFConstantK + float64(rank+1))
	}

	var results []HybridResult
	for docID, h := range merged {
		txt, _ := hse.bm25.GetRawText(docID)
		results = append(results, HybridResult{
			DocID:       docID,
			RRFScore:    h.rrfScore,
			DenseRank:   h.denseRank,
			SparseRank:  h.sparseRank,
			DenseDist:   h.denseDist,
			SparseScore: h.sparseScore,
			Text:        txt,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}
