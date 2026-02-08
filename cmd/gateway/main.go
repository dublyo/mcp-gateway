package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dublyo/mcp-gateway/internal/gateway"
	"github.com/dublyo/mcp-gateway/internal/server"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Dublyo MCP Gateway...")

	// Validate required env
	if os.Getenv("GATEWAY_TOKEN") == "" {
		log.Fatal("GATEWAY_TOKEN environment variable is required")
	}

	// Create gateway
	gw := gateway.New()

	// Create and start poller (config sync + metrics)
	poller := gateway.NewPoller(gw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Start(ctx)

	// Create and start HTTP server
	srv := server.New(gw)

	// Graceful shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("Shutting down gateway...")
		cancel()
		os.Exit(0)
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
