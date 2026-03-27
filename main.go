package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	port := flag.Int("port", 80, "server port")
	dataDir := flag.String("data", "./data", "data directory")
	baseURL := flag.String("base-url", "http://localhost", "public base URL for generated links")
	flag.Parse()

	store, err := NewStore(*dataDir)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer store.Close()

	limiter := NewRateLimiter(10, time.Minute)
	handlers := NewHandlers(store, limiter, *baseURL)

	tmpl := loadTemplates()

	mux := http.NewServeMux()
	handlers.RegisterAPI(mux)
	handlers.RegisterWeb(mux, tmpl)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("share-service starting on %s (data=%s, base-url=%s)", addr, *dataDir, *baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
