// Command server runs the DFlow HTTP API: it serves the Dify-DSL editor
// backend (flow CRUD, publish/versioning, import/export, and interactive test
// runs with per-node execution tracing over SSE).
//
// Usage:
//
//	go run ./cmd/server -addr :8080 -data ./data
//
// By default interactive test runs use a deterministic MockLLM so the editor
// works fully offline. Wire your own engine.LLMClient / engine.ToolClient here
// (replacing mockLLM) to hit real models.
package main

import (
	"flag"
	"log"
	"net/http"

	"dify-dsl-parser/engine"
	"dify-dsl-parser/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./data", "flow storage directory")
	flag.Parse()

	store, err := server.NewStore(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	// Offline default: deterministic mock so llm/answer nodes produce output
	// during interactive tests without any API keys. Swap for a real client.
	llm := &engine.MockLLM{Reply: "(mock LLM reply) configure a real LLMClient in cmd/server/main.go"}
	tool := &engine.MockTool{}

	srv := server.New(store, llm, tool)

	log.Printf("DFlow API listening on %s (data: %s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
