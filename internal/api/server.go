package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/letsgolearn/p2data/internal/pfilter"
	"github.com/letsgolearn/p2data/internal/redact"
)

// supportedLabels are the base English model's entity groups with friendly tags.
var supportedLabels = []labelInfo{
	{"private_person", "PERSON"},
	{"private_email", "EMAIL"},
	{"private_phone", "PHONE"},
	{"private_address", "ADDRESS"},
	{"private_date", "DATE"},
	{"private_url", "URL"},
	{"account_number", "ACCOUNT"},
	{"secret", "SECRET"},
}

type labelInfo struct {
	Label string `json:"label"`
	Tag   string `json:"tag"`
}

// Options configures the HTTP server.
type Options struct {
	Classifier       pfilter.Classifier
	Redactor         *redact.Redactor
	APIKeys          []string
	MaxBodyBytes     int64
	DefaultThreshold float32
	Ready            func() bool // reports whether the engine is loaded
	Logger           *slog.Logger
}

// Server holds the wired HTTP handler.
type Server struct {
	opts Options
	mux  *http.ServeMux
}

// New builds a Server with all routes and middleware wired.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{opts: opts, mux: http.NewServeMux()}

	// Health endpoints are unauthenticated.
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if opts.Ready != nil && !opts.Ready() {
			writeError(w, http.StatusServiceUnavailable, "engine not ready")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Authenticated API.
	auth := apiKeyAuth(opts.APIKeys)
	s.mux.Handle("GET /v1/labels", auth(http.HandlerFunc(s.handleLabels)))
	s.mux.Handle("POST /v1/redact", auth(http.HandlerFunc(s.handleRedact)))

	return s
}

// Handler returns the fully wrapped http.Handler (logging + recovery applied to
// every route, including health checks).
func (s *Server) Handler() http.Handler {
	return chain(s.mux, accessLog(s.opts.Logger), recoverer(s.opts.Logger))
}

func (s *Server) handleLabels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"labels": supportedLabels})
}

type redactRequest struct {
	Text      string         `json:"text"`
	Threshold *float32       `json:"threshold,omitempty"`
	Policy    *redact.Policy `json:"policy,omitempty"`
}

type redactResponse struct {
	Redacted string           `json:"redacted"`
	Entities []pfilter.Entity `json:"entities"`
	Counts   map[string]int   `json:"counts"`
}

func (s *Server) handleRedact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req redactRequest
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "field 'text' is required")
		return
	}

	policy := redact.Policy{}
	if req.Policy != nil {
		policy = *req.Policy
	}
	if err := policy.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	threshold := s.opts.DefaultThreshold
	if req.Threshold != nil {
		threshold = *req.Threshold
	}

	ents, err := s.opts.Classifier.Classify(r.Context(), req.Text, threshold)
	if err != nil {
		s.opts.Logger.Error("classify failed", "err", err)
		writeError(w, http.StatusInternalServerError, "classification failed")
		return
	}

	redacted, applied := s.opts.Redactor.Apply(req.Text, ents, policy)

	counts := map[string]int{}
	for _, e := range applied {
		counts[e.Label]++
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].Start < applied[j].Start })

	// Record metadata for the access log (counts only, never text).
	if ls := logStateFrom(r.Context()); ls != nil {
		ls.entities = len(applied)
		ls.labels = counts
	}

	writeJSON(w, http.StatusOK, redactResponse{
		Redacted: redacted,
		Entities: applied,
		Counts:   counts,
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- log state plumbing ---

type ctxKey int

const logStateKey ctxKey = iota

type logState struct {
	entities int
	labels   map[string]int
}

func withLogState(ctx context.Context) (context.Context, *logState) {
	ls := &logState{}
	return context.WithValue(ctx, logStateKey, ls), ls
}

func logStateFrom(ctx context.Context) *logState {
	ls, _ := ctx.Value(logStateKey).(*logState)
	return ls
}

func logFields(ctx context.Context) []any {
	ls := logStateFrom(ctx)
	if ls == nil || ls.entities == 0 {
		return nil
	}
	return []any{"redacted_entities", ls.entities, "labels", ls.labels}
}
