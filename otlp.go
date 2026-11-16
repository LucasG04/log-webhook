package main

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Minimal OTLP/JSON shapes for ExportLogsServiceRequest. Only the fields
// needed for flattening are declared; unknown fields are ignored.

type ExportLogsServiceRequest struct {
	ResourceLogs []ResourceLogs `json:"resourceLogs"`
}

type ResourceLogs struct {
	Resource  Resource    `json:"resource"`
	ScopeLogs []ScopeLogs `json:"scopeLogs"`
}

type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

type ScopeLogs struct {
	Scope      Scope       `json:"scope"`
	LogRecords []LogRecord `json:"logRecords"`
}

type Scope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type LogRecord struct {
	TimeUnixNano         numString  `json:"timeUnixNano"`
	ObservedTimeUnixNano numString  `json:"observedTimeUnixNano"`
	SeverityNumber       int32      `json:"severityNumber"`
	SeverityText         string     `json:"severityText"`
	Body                 *AnyValue  `json:"body"`
	Attributes           []KeyValue `json:"attributes"`
	TraceID              string     `json:"traceId"`
	SpanID               string     `json:"spanId"`
}

type KeyValue struct {
	Key   string   `json:"key"`
	Value AnyValue `json:"value"`
}

type AnyValue struct {
	StringValue *string       `json:"stringValue"`
	BoolValue   *bool         `json:"boolValue"`
	IntValue    *numString    `json:"intValue"`
	DoubleValue *numString    `json:"doubleValue"`
	BytesValue  *string       `json:"bytesValue"` // base64, kept as-is
	ArrayValue  *ArrayValue   `json:"arrayValue"`
	KvlistValue *KeyValueList `json:"kvlistValue"`
}

type ArrayValue struct {
	Values []AnyValue `json:"values"`
}

type KeyValueList struct {
	Values []KeyValue `json:"values"`
}

// numString holds the textual form of a JSON number that OTLP/JSON may encode
// either as a string ("123", "NaN") or as a bare number (123).
type numString string

func (n *numString) UnmarshalJSON(b []byte) error {
	if b[0] == '"' {
		return json.Unmarshal(b, (*string)(n))
	}
	if string(b) != "null" {
		*n = numString(b)
	}
	return nil
}

// String converts an AnyValue to a string: scalars verbatim, arrays and
// kvlists as JSON.
func (v *AnyValue) String() string {
	if v == nil {
		return ""
	}
	switch n := v.native().(type) {
	case nil:
		return ""
	case string:
		return n
	case bool:
		return strconv.FormatBool(n)
	case json.Number:
		return string(n)
	default:
		b, _ := marshal(n) // cannot fail: only strings, bools, valid numbers, slices and maps
		return string(b)
	}
}

// native converts an AnyValue to plain Go values for JSON serialization.
func (v *AnyValue) native() any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.IntValue != nil:
		return number(*v.IntValue)
	case v.DoubleValue != nil:
		return number(*v.DoubleValue)
	case v.BytesValue != nil:
		return *v.BytesValue
	case v.ArrayValue != nil:
		out := make([]any, len(v.ArrayValue.Values))
		for i := range v.ArrayValue.Values {
			out[i] = v.ArrayValue.Values[i].native()
		}
		return out
	case v.KvlistValue != nil:
		out := make(map[string]any, len(v.KvlistValue.Values))
		for _, kv := range v.KvlistValue.Values {
			out[kv.Key] = kv.Value.native()
		}
		return out
	}
	return nil
}

// number returns a json.Number for valid JSON number literals and the plain
// string otherwise (e.g. "NaN", "Infinity").
func number(n numString) any {
	s := string(n)
	if s != "" && (s[0] == '-' || (s[0] >= '0' && s[0] <= '9')) && json.Valid([]byte(s)) {
		return json.Number(s)
	}
	return s
}

// marshal is json.Marshal without HTML escaping and without trailing newline.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
