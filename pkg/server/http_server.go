package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"project-x/pkg/server/ui"
)

type HTTPServer struct {
	engine    *Engine
	server    *http.Server
	startTime time.Time
}

type KVPutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type VectorInsertRequest struct {
	ID     uint64    `json:"id"`
	Vector []float32 `json:"vector"`
}

type VectorSearchRequest struct {
	Vector []float32 `json:"vector"`
	K      int       `json:"k"`
}

type VectorSearchResponse struct {
	Results []SearchResultItem `json:"results"`
	Latency string             `json:"latency"`
}

type SearchResultItem struct {
	ID       uint64  `json:"id"`
	Distance float32 `json:"distance"`
}

type AIChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type MemoryStoreRequest struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Importance float32 `json:"importance"`
}

func NewHTTPServer(addr string, engine *Engine) *HTTPServer {
	mux := http.NewServeMux()
	hs := &HTTPServer{
		engine:    engine,
		startTime: time.Now(),
	}

	// Web UI
	mux.Handle("/", ui.Handler())

	// Health & Metrics
	mux.HandleFunc("/healthz", hs.handleHealthz)

	// KV Storage Endpoints
	mux.HandleFunc("/v1/kv/put", hs.handleKVPut)
	mux.HandleFunc("/v1/kv/get", hs.handleKVGet)
	mux.HandleFunc("/v1/kv/del", hs.handleKVDel)

	// Vector Storage & Search Endpoints
	mux.HandleFunc("/v1/vector/insert", hs.handleVectorInsert)
	mux.HandleFunc("/v1/vector/search", hs.handleVectorSearch)

	// Hybrid Search Endpoint (BM25 + HNSW RRF)
	mux.HandleFunc("/v1/search/hybrid", hs.handleHybridSearch)

	// AI Agent & Semantic Memory Endpoints
	mux.HandleFunc("/v1/ai/chat", hs.handleAIChat)
	mux.HandleFunc("/v1/ai/memory/store", hs.handleAIMemoryStore)
	mux.HandleFunc("/v1/ai/memory/recall", hs.handleAIMemoryRecall)

	hs.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return hs
}

func (hs *HTTPServer) Start() error {
	return hs.server.ListenAndServe()
}

func (hs *HTTPServer) Close() error {
	return hs.server.Close()
}

func (hs *HTTPServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"node_id":      hs.engine.cfg.NodeID,
		"uptime_sec":   time.Since(hs.startTime).Seconds(),
		"vector_count": hs.engine.VectorCount(),
		"memory_count": hs.engine.MemoryStore().Count(),
	})
}

func (hs *HTTPServer) handleKVPut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req KVPutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	key := []byte(req.Key)
	val := []byte(req.Value)

	if err := hs.engine.Put(key, val); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (hs *HTTPServer) handleKVGet(w http.ResponseWriter, r *http.Request) {
	keyStr := r.URL.Query().Get("key")
	if keyStr == "" {
		http.Error(w, "missing 'key' query parameter", http.StatusBadRequest)
		return
	}

	val, found, err := hs.engine.Get([]byte(keyStr))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"key":   keyStr,
		"value": string(val),
		"raw":   base64.StdEncoding.EncodeToString(val),
	})
}

func (hs *HTTPServer) handleKVDel(w http.ResponseWriter, r *http.Request) {
	keyStr := r.URL.Query().Get("key")
	if keyStr == "" {
		http.Error(w, "missing 'key' query parameter", http.StatusBadRequest)
		return
	}

	if err := hs.engine.Delete([]byte(keyStr)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"deleted"}`))
}

func (hs *HTTPServer) handleVectorInsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req VectorInsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Vector) != hs.engine.cfg.VectorDim {
		http.Error(w, fmt.Sprintf("invalid vector dimension: expected %d, got %d", hs.engine.cfg.VectorDim, len(req.Vector)), http.StatusBadRequest)
		return
	}

	hs.engine.InsertVector(req.ID, req.Vector)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"inserted"}`))
}

func (hs *HTTPServer) handleVectorSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req VectorSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.K <= 0 {
		req.K = 10
	}

	start := time.Now()
	items := hs.engine.SearchVector(req.Vector, req.K)
	latency := time.Since(start)

	results := make([]SearchResultItem, len(items))
	for i, it := range items {
		results[i] = SearchResultItem{
			ID:       it.ID,
			Distance: it.Dist,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(VectorSearchResponse{
		Results: results,
		Latency: latency.String(),
	})
}

func (hs *HTTPServer) handleHybridSearch(w http.ResponseWriter, r *http.Request) {
	queryText := r.URL.Query().Get("q")
	kStr := r.URL.Query().Get("k")
	topK := 10
	if k, err := strconv.Atoi(kStr); err == nil && k > 0 {
		topK = k
	}

	results := hs.engine.HybridSearch(queryText, nil, topK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"query":   queryText,
		"results": results,
		"count":   len(results),
	})
}

func (hs *HTTPServer) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default_session"
	}

	resp, err := hs.engine.Agent().Chat(req.SessionID, req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (hs *HTTPServer) handleAIMemoryStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MemoryStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	item, err := hs.engine.MemoryStore().Store(req.Content, req.Category, req.Importance)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func (hs *HTTPServer) handleAIMemoryRecall(w http.ResponseWriter, r *http.Request) {
	queryText := r.URL.Query().Get("q")
	kStr := r.URL.Query().Get("k")
	topK := 5
	if k, err := strconv.Atoi(kStr); err == nil && k > 0 {
		topK = k
	}

	memories := hs.engine.MemoryStore().Recall(queryText, topK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"query":    queryText,
		"memories": memories,
		"count":    len(memories),
	})
}
