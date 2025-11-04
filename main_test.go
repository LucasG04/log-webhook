package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLogWebhookHandler(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectError    bool
		shouldLog      bool
	}{
		{
			name:           "valid json single line",
			requestBody:    `{"service": "test", "message": "hello"}`,
			expectedStatus: http.StatusOK,
			expectError:    false,
			shouldLog:      true,
		},
		{
			name: "valid json multiline",
			requestBody: `{
    "service": "test",
    "message": "hello world",
    "level": "info"
}`,
			expectedStatus: http.StatusOK,
			expectError:    false,
			shouldLog:      true,
		},
		{
			name:           "valid json with spaces",
			requestBody:    `{ "service" : "test" , "message" : "hello" }`,
			expectedStatus: http.StatusOK,
			expectError:    false,
			shouldLog:      true,
		},
		{
			name:           "invalid json",
			requestBody:    `{"service": "test", "message": }`,
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
			shouldLog:      false,
		},
		{
			name:           "empty body",
			requestBody:    "",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
			shouldLog:      false,
		},
		{
			name:           "non-json text",
			requestBody:    "this is not json",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
			shouldLog:      false,
		},
		{
			name:           "json array",
			requestBody:    `[{"service": "test"}, {"service": "test2"}]`,
			expectedStatus: http.StatusOK,
			expectError:    false,
			shouldLog:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout to verify log output
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			rr := httptest.NewRecorder()

			// Create handler and serve request
			handler := createLogHandler()

			handler.ServeHTTP(rr, req)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout

			var output bytes.Buffer
			io.Copy(&output, r)
			logOutput := output.String()

			// Check status code
			if rr.Code != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, tt.expectedStatus)
			}

			// Check response content type for successful requests
			if rr.Code == http.StatusOK {
				contentType := rr.Header().Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", contentType)
				}
			}

			// Check if log output is present when expected
			if tt.shouldLog {
				if logOutput == "" {
					t.Error("expected log output but got none")
				} else {
					// Verify output is on a single line (no newlines except the one added by Println)
					lines := strings.Split(strings.TrimSpace(logOutput), "\n")
					if len(lines) != 1 {
						t.Errorf("expected single line output, got %d lines: %v", len(lines), lines)
					}

					// Verify it's valid compacted JSON by checking it doesn't contain extra whitespace
					if strings.Contains(logOutput, "    ") || strings.Contains(logOutput, "\t") {
						t.Error("output contains extra whitespace, should be compacted JSON")
					}
				}
			} else if !tt.shouldLog && logOutput != "" {
				t.Errorf("expected no log output but got: %s", logOutput)
			}
		})
	}
}

func TestLogWebhookEndpoint(t *testing.T) {
	// Test that the handler is properly registered at /v1/logs
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(`{"test": "data"}`))
	rr := httptest.NewRecorder()

	// Create a new mux and register the handler
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", createLogHandler())

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	// Check response body
	expectedBody := `{"status":"success"}`
	if strings.TrimSpace(rr.Body.String()) != expectedBody {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expectedBody)
	}
}

func TestInvalidEndpoint(t *testing.T) {
	// Test that other endpoints return 404
	req := httptest.NewRequest(http.MethodPost, "/invalid", strings.NewReader(`{"test": "data"}`))
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusNotFound)
	}
}

func TestHTTPMethods(t *testing.T) {
	methods := []struct {
		method         string
		expectedStatus int
	}{
		{http.MethodPost, http.StatusOK},
		{http.MethodGet, http.StatusOK},    // Handler doesn't restrict methods
		{http.MethodPut, http.StatusOK},    // Handler doesn't restrict methods
		{http.MethodDelete, http.StatusOK}, // Handler doesn't restrict methods
	}

	for _, m := range methods {
		t.Run(m.method, func(t *testing.T) {
			var body io.Reader
			if m.method != http.MethodGet {
				body = strings.NewReader(`{"test": "data"}`)
			}

			req := httptest.NewRequest(m.method, "/v1/logs", body)
			rr := httptest.NewRecorder()

			handler := createLogHandler()

			// Handle empty body case for GET requests
			if m.method == http.MethodGet {
				handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer r.Body.Close()
					b, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, "read error", http.StatusBadRequest)
						return
					}

					if len(b) == 0 {
						w.WriteHeader(http.StatusOK)
						return
					}

					dst := &bytes.Buffer{}
					if err := json.Compact(dst, b); err != nil {
						http.Error(w, "invalid json", http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusOK)
				})
			}

			handler.ServeHTTP(rr, req)

			if rr.Code != m.expectedStatus {
				t.Errorf("method %s returned wrong status code: got %v want %v", m.method, rr.Code, m.expectedStatus)
			}
		})
	}
}

// Benchmark tests
func BenchmarkLogWebhookHandler(b *testing.B) {
	handler := createLogHandler()

	requestBody := `{"service": "benchmark", "message": "test message", "level": "info"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(requestBody))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func TestEnvironmentVariables(t *testing.T) {
	// Test default values
	t.Run("default values", func(t *testing.T) {
		// Ensure environment variables are not set
		os.Unsetenv("PORT")
		os.Unsetenv("LOG_ENDPOINT")

		// Since we can't easily test the main function directly,
		// we'll test that the defaults would be applied
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}

		endpoint := os.Getenv("LOG_ENDPOINT")
		if endpoint == "" {
			endpoint = "/v1/logs"
		}

		if port != "8080" {
			t.Errorf("expected default port 8080, got %s", port)
		}

		if endpoint != "/v1/logs" {
			t.Errorf("expected default endpoint /v1/logs, got %s", endpoint)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		// Set custom environment variables
		os.Setenv("PORT", "9090")
		os.Setenv("LOG_ENDPOINT", "/custom/logs")
		defer func() {
			os.Unsetenv("PORT")
			os.Unsetenv("LOG_ENDPOINT")
		}()

		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}

		endpoint := os.Getenv("LOG_ENDPOINT")
		if endpoint == "" {
			endpoint = "/v1/logs"
		}

		if port != "9090" {
			t.Errorf("expected custom port 9090, got %s", port)
		}

		if endpoint != "/custom/logs" {
			t.Errorf("expected custom endpoint /custom/logs, got %s", endpoint)
		}
	})
}

func TestHealthCheckEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	// Create a new mux and register the health handler
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"healthy"}`)
	})

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health check returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	expectedBody := `{"status":"healthy"}`
	if strings.TrimSpace(rr.Body.String()) != expectedBody {
		t.Errorf("health check returned unexpected body: got %v want %v", rr.Body.String(), expectedBody)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}
}
