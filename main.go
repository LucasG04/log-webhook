package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// createLogHandler creates the log webhook handler
func createLogHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set response headers
		w.Header().Set("Content-Type", "application/json")

		defer r.Body.Close()

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, `{"error":"Failed to read request body"}`, http.StatusBadRequest)
			return
		}

		// Validate and compact JSON
		compactedJSON := &bytes.Buffer{}
		if err := json.Compact(compactedJSON, body); err != nil {
			log.Printf("Invalid JSON received: %v", err)
			http.Error(w, `{"error":"Invalid JSON format"}`, http.StatusBadRequest)
			return
		}

		// Log the compacted JSON
		fmt.Println(compactedJSON.String())

		// Send success response
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"success"}`)
	}
}

func main() {
	// Get configuration from environment variables with defaults
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	endpoint := os.Getenv("LOG_ENDPOINT")
	if endpoint == "" {
		endpoint = "/v1/logs"
	}

	// Create HTTP server with timeouts
	mux := http.NewServeMux()
	mux.HandleFunc(endpoint, createLogHandler())

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"healthy"}`)
	})

	log.Printf("log-webhook listening on :%s at endpoint %s", port, endpoint)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
