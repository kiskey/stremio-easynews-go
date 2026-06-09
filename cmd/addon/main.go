package main

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/kiskey/stremio-easynews-go/internal/server"
)

func main() {
	// Load environment variables from a local .env file.
	// Safe to ignore errors in production where settings are defined natively.
	_ = godotenv.Load()

	port := 1337
	if pStr := os.Getenv("PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			port = p
		}
	}

	// Handover execution to the HTTP server gateway
	server.ServeHTTP(port)
}
