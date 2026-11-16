package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LogHandler receives OTLP/JSON log exports and writes one JSON line per log
// record to Out.
type LogHandler struct {
	Out             io.Writer // receives each request's lines in one Write
	MaxBodyBytes    int64     // applies to raw and decompressed body
	MaxMessageBytes int
	FieldPrefix     string // if set, each line is {"<prefix>": record}
	Logger          *slog.Logger
	Now             func() time.Time

	mu sync.Mutex // serializes writes to Out
}

func (h *LogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.fail(w, r, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		h.fail(w, r, http.StatusUnsupportedMediaType, "content type must be application/json", nil)
		return
	}

	body := http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(body)
		if err != nil {
			h.fail(w, r, statusFor(err), "invalid gzip body", err)
			return
		}
		defer gz.Close()
		body = http.MaxBytesReader(w, gz, h.MaxBodyBytes) // guards against gzip bombs
	default:
		h.fail(w, r, http.StatusUnsupportedMediaType, "unsupported content encoding", nil)
		return
	}

	// Read to EOF so trailing data is rejected and the gzip checksum is verified.
	data, err := io.ReadAll(body)
	if err != nil {
		h.fail(w, r, statusFor(err), "invalid request body", err)
		return
	}
	var req ExportLogsServiceRequest
	if err := json.Unmarshal(data, &req); err != nil {
		h.fail(w, r, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if err := h.write(Flatten(&req, h.Now(), h.MaxMessageBytes)); err != nil {
		h.fail(w, r, http.StatusInternalServerError, "failed to write records", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, "{}")
}

func (h *LogHandler) write(records []Record) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode appends the newline that terminates each line
	enc.SetEscapeHTML(false)
	for _, rec := range records {
		var v any = rec
		if h.FieldPrefix != "" {
			v = map[string]Record{h.FieldPrefix: rec}
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.Out.Write(buf.Bytes())
	return err
}

func (h *LogHandler) fail(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	attrs := []any{"status", status, "method", r.Method, "path", r.URL.Path, "contentLength", r.ContentLength}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	h.Logger.Warn(msg, attrs...)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := marshal(map[string]string{"message": msg})
	w.Write(b)
}

func statusFor(err error) int {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
