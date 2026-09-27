package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"project-x/pkg/server"
	"project-x/pkg/vector/distance"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server listening port")
	dataDir := flag.String("dir", "./data", "Storage data directory")
	nodeID := flag.Uint64("node-id", 1, "Raft cluster node ID")
	vectorDim := flag.Int("dim", 128, "Vector dimensionality")
	flag.Parse()

	log.Printf("======================================================================")
	log.Printf(" Distributed LSM-Tree & Vector Database Engine (Project-X) ")
	log.Printf(" Node ID: %d | Vector Dim: %d | Data Dir: %s", *nodeID, *vectorDim, *dataDir)
	log.Printf("======================================================================")

	cfg := server.Config{
		DataDir:        *dataDir,
		NodeID:         *nodeID,
		VectorDim:      *vectorDim,
		VectorMetric:   distance.L2,
		MemTableSize:   64 * 1024 * 1024, // 64 MB
		WALBatchWindow: 50 * time.Microsecond,
	}

	eng, err := server.OpenEngine(cfg)
	if err != nil {
		log.Fatalf("failed to open storage engine: %v", err)
	}
	defer eng.Close()

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	srv := server.NewHTTPServer(addr, eng)

	go func() {
		log.Printf("Server listening on HTTP %s ...", addr)
		if err := srv.Start(); err != nil && err != os.ErrClosed {
			log.Printf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("Shutting down storage engine gracefully...")
	_ = srv.Close()
	_ = eng.Close()
	log.Printf("Shutdown complete.")
}
