package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fixture = `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"donkey-frontend"}}]},"scopeLogs":[{"scope":{"name":"donkey-frontend"},"logRecords":[{"timeUnixNano":"1794827798817000000","observedTimeUnixNano":"1794827798817000000","severityNumber":9,"severityText":"INFO","body":{"stringValue":"User DONKEYKOORD1 logged in"}},{"timeUnixNano":"1794827798823000000","observedTimeUnixNano":"1794827798823000000","severityNumber":17,"severityText":"ERROR","body":{"stringValue":"Version \"develop\" from localstorage not valid"},"attributes":[{"key":"version","value":{"stringValue":"develop"}}]}]}]}]}`

var testNow = time.Date(2030, 1, 2, 3, 4, 5, 678_000_000, time.UTC)

func flattenJSON(t *testing.T, s string, maxMessageBytes int) []Record {
	t.Helper()
	var req ExportLogsServiceRequest
	if err := json.Unmarshal([]byte(s), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return Flatten(&req, testNow, maxMessageBytes)
}

func TestFlattenFixture(t *testing.T) {
	res := map[string]string{"service.name": "donkey-frontend"}
	want := []Record{
		{
			Timestamp: "2026-11-16T11:16:38.817Z", Level: "INFO", SeverityNumber: 9,
			Message: "User DONKEYKOORD1 logged in", Service: "donkey-frontend", Scope: "donkey-frontend",
			Resource: res,
		},
		{
			Timestamp: "2026-11-16T11:16:38.823Z", Level: "ERROR", SeverityNumber: 17,
			Message: `Version "develop" from localstorage not valid`, Service: "donkey-frontend", Scope: "donkey-frontend",
			Resource: res, Attributes: map[string]string{"version": "develop"},
		},
	}
	if got := flattenJSON(t, fixture, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestFlattenAssociation(t *testing.T) {
	in := `{"resourceLogs":[
	  {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc-a"}}]},"scopeLogs":[
	    {"scope":{"name":"a1","version":"1.0"},"logRecords":[{"body":{"stringValue":"a1-1"}},{"body":{"stringValue":"a1-2"}}]},
	    {"scope":{"name":"a2"},"logRecords":[{"body":{"stringValue":"a2-1"}}]}]},
	  {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc-b"}},{"key":"k8s.pod.name","value":{"stringValue":"pod-b"}}]},"scopeLogs":[
	    {"scope":{"name":"b1"},"logRecords":[{"body":{"stringValue":"b1-1"}}]}]},
	  {"resource":{},"scopeLogs":[{"scope":{},"logRecords":[{"body":{"stringValue":"none"}}]}]}]}`

	want := []struct{ msg, service, scope, version, pod string }{
		{"a1-1", "svc-a", "a1", "1.0", ""},
		{"a1-2", "svc-a", "a1", "1.0", ""},
		{"a2-1", "svc-a", "a2", "", ""},
		{"b1-1", "svc-b", "b1", "", "pod-b"},
		{"none", "", "", "", ""},
	}
	got := flattenJSON(t, in, 0)
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Message != w.msg || g.Service != w.service || g.Scope != w.scope || g.ScopeVersion != w.version || g.Resource["k8s.pod.name"] != w.pod {
			t.Errorf("record %d: got %+v, want %+v", i, g, w)
		}
	}
}

func TestAnyValue(t *testing.T) {
	tests := []struct {
		name, value, want string
	}{
		{"string", `{"stringValue":"hello <b>&"}`, "hello <b>&"},
		{"empty string", `{"stringValue":""}`, ""},
		{"bool", `{"boolValue":true}`, "true"},
		{"int as string", `{"intValue":"9007199254740993"}`, "9007199254740993"},
		{"int as number", `{"intValue":42}`, "42"},
		{"double", `{"doubleValue":1.5}`, "1.5"},
		{"double as string", `{"doubleValue":"NaN"}`, "NaN"},
		{"bytes", `{"bytesValue":"aGVsbG8="}`, "aGVsbG8="},
		{"empty", `{}`, ""},
		{"array", `{"arrayValue":{"values":[{"stringValue":"a"},{"intValue":"1"},{"boolValue":false},{"doubleValue":"Infinity"}]}}`, `["a",1,false,"Infinity"]`},
		{"nested array", `{"arrayValue":{"values":[{"arrayValue":{"values":[{"intValue":"1"}]}},{"arrayValue":{}}]}}`, `[[1],[]]`},
		{"kvlist", `{"kvlistValue":{"values":[{"key":"b","value":{"stringValue":"x"}},{"key":"a","value":{"doubleValue":2.5}}]}}`, `{"a":2.5,"b":"x"}`},
		{"nested kvlist", `{"kvlistValue":{"values":[{"key":"user","value":{"kvlistValue":{"values":[{"key":"roles","value":{"arrayValue":{"values":[{"stringValue":"admin"}]}}}]}}}]}}`, `{"user":{"roles":["admin"]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":` + tt.value + `,"attributes":[{"key":"k","value":` + tt.value + `}]}]}]}]}`
			got := flattenJSON(t, in, 0)
			if len(got) != 1 {
				t.Fatalf("got %d records", len(got))
			}
			if got[0].Message != tt.want {
				t.Errorf("body: got %q, want %q", got[0].Message, tt.want)
			}
			if got[0].Attributes["k"] != tt.want {
				t.Errorf("attribute: got %q, want %q", got[0].Attributes["k"], tt.want)
			}
		})
	}
}

func TestTimestamp(t *testing.T) {
	tests := []struct {
		name, record, want string
	}{
		{"time", `{"timeUnixNano":"1794827798823000000","observedTimeUnixNano":"1794827799000000000"}`, "2026-11-16T11:16:38.823Z"},
		{"time as number", `{"timeUnixNano":1794827798823000000}`, "2026-11-16T11:16:38.823Z"},
		{"observed fallback", `{"observedTimeUnixNano":"1794827799000000000"}`, "2026-11-16T11:16:39.000Z"},
		{"zero time uses observed", `{"timeUnixNano":"0","observedTimeUnixNano":"1794827799000000000"}`, "2026-11-16T11:16:39.000Z"},
		{"now fallback", `{}`, "2030-01-02T03:04:05.678Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flattenJSON(t, `{"resourceLogs":[{"scopeLogs":[{"logRecords":[`+tt.record+`]}]}]}`, 0)
			if got[0].Timestamp != tt.want {
				t.Errorf("got %s, want %s", got[0].Timestamp, tt.want)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	tests := []struct {
		number int32
		text   string
		want   string
	}{
		{0, "", "UNSPECIFIED"},
		{1, "", "TRACE"}, {4, "", "TRACE"},
		{5, "", "DEBUG"}, {8, "", "DEBUG"},
		{9, "", "INFO"}, {12, "", "INFO"},
		{13, "", "WARN"}, {16, "", "WARN"},
		{17, "", "ERROR"}, {20, "", "ERROR"},
		{21, "", "FATAL"}, {24, "", "FATAL"},
		{25, "", "UNSPECIFIED"},
		{17, "Fehler", "Fehler"},
	}
	for _, tt := range tests {
		if got := level(LogRecord{SeverityNumber: tt.number, SeverityText: tt.text}); got != tt.want {
			t.Errorf("level(%d, %q) = %s, want %s", tt.number, tt.text, got, tt.want)
		}
	}
}

func TestTruncation(t *testing.T) {
	tests := []struct {
		name, msg string
		max       int
		want      string
		wantTrunc string
	}{
		{"under limit", "abc", 5, "abc", ""},
		{"at limit", "abcde", 5, "abcde", ""},
		{"over limit", "abcdefgh", 5, "abcde" + truncationMarker, "true"},
		{"utf8 boundary", "ääää", 3, "ä" + truncationMarker, "true"},
		{"disabled", strings.Repeat("x", 100), 0, strings.Repeat("x", 100), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"` + tt.msg + `"}}]}]}]}`
			got := flattenJSON(t, in, tt.max)[0]
			if got.Message != tt.want || got.Truncated != tt.wantTrunc {
				t.Errorf("got (%q, %q), want (%q, %q)", got.Message, got.Truncated, tt.want, tt.wantTrunc)
			}
		})
	}
}
