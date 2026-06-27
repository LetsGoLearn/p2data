package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/letsgolearn/p2data/internal/pfilter"
	"github.com/letsgolearn/p2data/internal/redact"
)

const testKey = "secret-test-key"

func testServer() http.Handler {
	s := New(Options{
		Classifier:       pfilter.NewFake("Jane Doe"),
		Redactor:         redact.New("", []byte("k")),
		APIKeys:          []string{testKey},
		MaxBodyBytes:     1 << 16,
		DefaultThreshold: 0.5,
		Ready:            func() bool { return true },
	})
	return s.Handler()
}

func doRedact(t *testing.T, h http.Handler, key, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/redact", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestRedact_PersonKeepFirst(t *testing.T) {
	h := testServer()
	body := `{"text":"Email Jane Doe at jane@acme.com","policy":{"byLabel":{"private_person":"keepFirst"}}}`
	resp := doRedact(t, h, testKey, body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out redactResponse
	mustDecode(t, resp.Body, &out)
	if out.Redacted != "Email Jane [LAST] at [EMAIL]" {
		t.Fatalf("redacted=%q", out.Redacted)
	}
	if out.Counts["private_email"] != 1 || out.Counts["private_person"] != 1 {
		t.Fatalf("counts=%v", out.Counts)
	}
	// No raw PII should appear anywhere in the response payload.
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("jane@acme.com")) || bytes.Contains(raw, []byte("Doe")) {
		t.Fatalf("response leaked PII: %s", raw)
	}
}

func TestRedact_RequiresAuth(t *testing.T) {
	h := testServer()
	resp := doRedact(t, h, "", `{"text":"x"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key: status=%d want 401", resp.StatusCode)
	}
	resp = doRedact(t, h, "wrong", `{"text":"x"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad key: status=%d want 401", resp.StatusCode)
	}
}

func TestRedact_BadJSONAndMissingText(t *testing.T) {
	h := testServer()
	if resp := doRedact(t, h, testKey, `{`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json: status=%d", resp.StatusCode)
	}
	if resp := doRedact(t, h, testKey, `{"text":""}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text: status=%d", resp.StatusCode)
	}
	if resp := doRedact(t, h, testKey, `{"text":"x","policy":{"default":"bogus"}}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad mode: status=%d", resp.StatusCode)
	}
}

func TestRedact_BodyTooLarge(t *testing.T) {
	h := testServer()
	big := `{"text":"` + strings.Repeat("a", 1<<17) + `"}`
	resp := doRedact(t, h, testKey, big)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want 413", resp.StatusCode)
	}
}

func TestLabelsAndHealth(t *testing.T) {
	h := testServer()
	// labels requires auth
	req := httptest.NewRequest("GET", "/v1/labels", nil)
	req.Header.Set("X-API-Key", testKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("labels status=%d", rec.Code)
	}
	// health is open
	req = httptest.NewRequest("GET", "/healthz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("healthz status=%d", rec.Code)
	}
}

func mustDecode(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
