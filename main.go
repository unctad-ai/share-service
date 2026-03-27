package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 80, "server port")
	dataDir := flag.String("data", "./data", "data directory")
	baseURL := flag.String("base-url", "http://localhost", "public base URL for generated links")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("share-service starting on %s (data=%s, base-url=%s)", addr, *dataDir, *baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
