package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "Target server endpoint")
	action := flag.String("action", "health", "Action to perform: health, chat, put, get, hybrid, bench")
	query := flag.String("query", "", "Query or message payload")
	numOps := flag.Int("n", 1000, "Number of operations for benchmark")
	flag.Parse()

	client := &http.Client{Timeout: 10 * time.Second}

	switch *action {
	case "health":
		res, err := client.Get(*serverAddr + "/healthz")
		if err != nil {
			fmt.Printf("Health check failed: %v\n", err)
			os.Exit(1)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		fmt.Printf("Status: %s\nPayload: %s\n", res.Status, string(body))

	case "chat":
		if *query == "" {
			*query = "What storage and acceleration features are built into this node?"
		}
		reqBody, _ := json.Marshal(map[string]string{
			"session_id": "cli_session",
			"message":    *query,
		})
		res, err := client.Post(*serverAddr+"/v1/ai/chat", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Printf("Chat error: %v\n", err)
			os.Exit(1)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, body, "", "  ")
		fmt.Printf("%s\n", pretty.String())

	case "hybrid":
		if *query == "" {
			*query = "storage"
		}
		res, err := client.Get(*serverAddr + "/v1/search/hybrid?q=" + *query)
		if err != nil {
			fmt.Printf("Search error: %v\n", err)
			os.Exit(1)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, body, "", "  ")
		fmt.Printf("%s\n", pretty.String())

	case "bench":
		fmt.Printf("Executing %d concurrent KV puts against %s ...\n", *numOps, *serverAddr)
		start := time.Now()
		for i := 0; i < *numOps; i++ {
			k := fmt.Sprintf("bench_key_%08d", i)
			v := fmt.Sprintf("payload_data_%d_%f", i, rand.Float64())
			b, _ := json.Marshal(map[string]string{"key": k, "value": v})
			_, _ = client.Post(*serverAddr+"/v1/kv/put", "application/json", bytes.NewReader(b))
		}
		elapsed := time.Since(start)
		iops := float64(*numOps) / elapsed.Seconds()
		fmt.Printf("Completed in %s | Throughput: %.2f req/sec\n", elapsed, iops)

	default:
		fmt.Printf("Unknown action '%s'. Supported: health, chat, put, get, hybrid, bench\n", *action)
	}
}
