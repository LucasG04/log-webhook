package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testMaxBody = 4096

func newTestHandler(prefix string) (*LogHandler, *bytes.Buffer, *bytes.Buffer) {
	out, logs := &bytes.Buffer{}, &bytes.Buffer{}
	return &LogHandler{
		Out:             out,
		MaxBodyBytes:    testMaxBody,
		MaxMessageBytes: 16384,
		FieldPrefix:     prefix,
		Logger:          slog.New(slog.NewJSONHandler(logs, nil)),
		Now:             func() time.Time { return testNow },
	}, out, logs
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(s))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lines splits output into lines and fails unless each is a valid JSON object.
func lines(t *testing.T, out string) []map[string]any {
	t.Helper()
	if out == "" {
		return nil
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", l, err)
		}
		res = append(res, m)
	}
	return res
}

func TestHandler(t *testing.T) {
	huge := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"` + strings.Repeat("x", testMaxBody) + `"}}]}]}]}`
	bomb := `{"resourceLogs":[]` + strings.Repeat(" ", 1<<20) + `}`
	badCRC := gzipped(t, fixture)
	badCRC[len(badCRC)-8] ^= 0xff // corrupt the CRC-32 trailer

	tests := []struct {
		name        string
		method      string
		contentType string
		encoding    string
		body        []byte
		wantStatus  int
		wantLines   int
	}{
		{"fixture", http.MethodPost, "application/json", "", []byte(fixture), 200, 2},
		{"fixture gzip", http.MethodPost, "application/json", "gzip", gzipped(t, fixture), 200, 2},
		{"content type with charset", http.MethodPost, "application/json; charset=utf-8", "", []byte(fixture), 200, 2},
		{"empty request", http.MethodPost, "application/json", "", []byte(`{}`), 200, 0},
		{"empty resourceLogs", http.MethodPost, "application/json", "", []byte(`{"resourceLogs":[]}`), 200, 0},
		{"invalid json", http.MethodPost, "application/json", "", []byte(`{"resourceLogs":[SECRET-PAYLOAD`), 400, 0},
		{"wrong field type", http.MethodPost, "application/json", "", []byte(`{"resourceLogs":"SECRET-PAYLOAD"}`), 400, 0},
		{"empty body", http.MethodPost, "application/json", "", nil, 400, 0},
		{"invalid gzip", http.MethodPost, "application/json", "gzip", []byte("SECRET-PAYLOAD"), 400, 0},
		{"wrong method", http.MethodGet, "application/json", "", nil, 405, 0},
		{"wrong content type", http.MethodPost, "text/plain", "", []byte(fixture), 415, 0},
		{"protobuf", http.MethodPost, "application/x-protobuf", "", []byte(fixture), 415, 0},
		{"missing content type", http.MethodPost, "", "", []byte(fixture), 415, 0},
		{"unsupported encoding", http.MethodPost, "application/json", "zstd", []byte(fixture), 415, 0},
		{"oversized body", http.MethodPost, "application/json", "", []byte(huge), 413, 0},
		{"gzip bomb", http.MethodPost, "application/json", "gzip", gzipped(t, bomb), 413, 0},
		{"trailing data", http.MethodPost, "application/json", "", []byte(fixture + `{"x":1}`), 400, 0},
		{"gzip checksum mismatch", http.MethodPost, "application/json", "gzip", badCRC, 400, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, out, logs := newTestHandler("")
			req := httptest.NewRequest(tt.method, "/v1/logs", bytes.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			if tt.encoding != "" {
				req.Header.Set("Content-Encoding", tt.encoding)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status %d, want %d (body %s)", rr.Code, tt.wantStatus, rr.Body)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("response Content-Type %q", ct)
			}
			if tt.wantStatus == 200 && rr.Body.String() != "{}" {
				t.Errorf("response body %q, want {}", rr.Body)
			}
			if got := len(lines(t, out.String())); got != tt.wantLines {
				t.Errorf("got %d lines, want %d: %s", got, tt.wantLines, out)
			}
			if strings.Contains(logs.String(), "SECRET-PAYLOAD") {
				t.Errorf("payload leaked into logs: %s", logs)
			}
		})
	}
}

// failOnce fails the first Write and appends afterwards.
type failOnce struct {
	failed bool
	bytes.Buffer
}

func (f *failOnce) Write(p []byte) (int, error) {
	if !f.failed {
		f.failed = true
		return 0, errors.New("broken pipe")
	}
	return f.Buffer.Write(p)
}

func TestHandlerRecoversAfterWriteError(t *testing.T) {
	h, _, _ := newTestHandler("")
	out := &failOnce{}
	h.Out = out
	for _, want := range []int{500, 200} {
		req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(fixture))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != want {
			t.Errorf("status %d, want %d", rr.Code, want)
		}
	}
	if got := len(lines(t, out.String())); got != 2 {
		t.Errorf("got %d lines after recovery, want 2", got)
	}
}

func TestHandlerOutputLine(t *testing.T) {
	want := `{"timestamp":"2026-11-16T11:16:38.823Z","level":"ERROR","severityNumber":17,"message":"Version \"develop\" from localstorage not valid","service":"donkey-frontend","scope":"donkey-frontend","resource":{"service.name":"donkey-frontend"},"attributes":{"version":"develop"}}`
	for _, prefix := range []string{"", "otel"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			h, out, _ := newTestHandler(prefix)
			req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(fixture))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(httptest.NewRecorder(), req)

			w := want
			if prefix != "" {
				w = `{"` + prefix + `":` + want + `}`
			}
			got := strings.Split(out.String(), "\n")
			if len(got) != 3 || got[2] != "" || got[1] != w {
				t.Errorf("got output\n%s\nwant second line\n%s", out, w)
			}
		})
	}
}

func TestHandlerConcurrency(t *testing.T) {
	h, out, _ := newTestHandler("")
	h.MaxBodyBytes = 1 << 20
	// Lines larger than the bufio buffer are written directly, maximising interleaving risk.
	big := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"` + strings.Repeat("y", 10000) + `"}},{"body":{"stringValue":"z"}}]}]}]}`

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			body := fixture
			if i%2 == 0 {
				body = big
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/logs", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != 200 {
				t.Errorf("status %d", rr.Code)
			}
		})
	}
	wg.Wait()

	if got := len(lines(t, out.String())); got != 2*n {
		t.Errorf("got %d lines, want %d", got, 2*n)
	}
}

func TestMux(t *testing.T) {
	h, _, _ := newTestHandler("")
	mux := newMux("/v1/logs", h)
	tests := []struct {
		method, path string
		wantStatus   int
		wantBody     string
	}{
		{http.MethodGet, "/health", 200, `{"status":"healthy"}`},
		{http.MethodPost, "/invalid", 404, ""},
		{http.MethodGet, "/v1/logs", 405, ""},
	}
	for _, tt := range tests {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(tt.method, tt.path, nil))
		if rr.Code != tt.wantStatus {
			t.Errorf("%s %s: status %d, want %d", tt.method, tt.path, rr.Code, tt.wantStatus)
		}
		if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
			t.Errorf("%s %s: body %q, want %q", tt.method, tt.path, rr.Body, tt.wantBody)
		}
	}
}
