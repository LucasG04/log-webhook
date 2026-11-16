package main

import (
	"strconv"
	"time"
	"unicode/utf8"
)

const truncationMarker = "…[truncated]"

// Record is one flattened log record, written as one JSON line.
type Record struct {
	Timestamp      string            `json:"timestamp"`
	Level          string            `json:"level"`
	SeverityNumber int32             `json:"severityNumber,omitempty"`
	Message        string            `json:"message"`
	Truncated      string            `json:"truncated,omitempty"`
	Service        string            `json:"service,omitempty"`
	Scope          string            `json:"scope,omitempty"`
	ScopeVersion   string            `json:"scopeVersion,omitempty"`
	TraceID        string            `json:"traceId,omitempty"`
	SpanID         string            `json:"spanId,omitempty"`
	Resource       map[string]string `json:"resource,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// Flatten turns an OTLP request into one Record per log record. now is used
// when a record carries no timestamp; maxMessageBytes <= 0 disables truncation.
func Flatten(req *ExportLogsServiceRequest, now time.Time, maxMessageBytes int) []Record {
	var out []Record
	for _, rl := range req.ResourceLogs {
		resource := toMap(rl.Resource.Attributes)
		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				rec := Record{
					Timestamp:      timestamp(lr, now),
					Level:          level(lr),
					SeverityNumber: lr.SeverityNumber,
					Message:        lr.Body.String(),
					Service:        resource["service.name"],
					Scope:          sl.Scope.Name,
					ScopeVersion:   sl.Scope.Version,
					TraceID:        lr.TraceID,
					SpanID:         lr.SpanID,
					Resource:       resource,
					Attributes:     toMap(lr.Attributes),
				}
				if maxMessageBytes > 0 && len(rec.Message) > maxMessageBytes {
					rec.Message = truncate(rec.Message, maxMessageBytes) + truncationMarker
					rec.Truncated = "true"
				}
				out = append(out, rec)
			}
		}
	}
	return out
}

func toMap(kvs []KeyValue) map[string]string {
	if len(kvs) == 0 {
		return nil
	}
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.String()
	}
	return m
}

func timestamp(lr LogRecord, now time.Time) string {
	t := now
	for _, s := range []numString{lr.TimeUnixNano, lr.ObservedTimeUnixNano} {
		if ns, err := strconv.ParseInt(string(s), 10, 64); err == nil && ns > 0 {
			t = time.Unix(0, ns)
			break
		}
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func level(lr LogRecord) string {
	if lr.SeverityText != "" {
		return lr.SeverityText
	}
	switch n := lr.SeverityNumber; {
	case n >= 1 && n <= 4:
		return "TRACE"
	case n >= 5 && n <= 8:
		return "DEBUG"
	case n >= 9 && n <= 12:
		return "INFO"
	case n >= 13 && n <= 16:
		return "WARN"
	case n >= 17 && n <= 20:
		return "ERROR"
	case n >= 21 && n <= 24:
		return "FATAL"
	}
	return "UNSPECIFIED"
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
