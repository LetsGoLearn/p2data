// Command redactor is a secure HTTP API that redacts PII from text using the
// privacy-filter.cpp NER engine (bound via cgo).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/letsgolearn/p2data/internal/api"
	"github.com/letsgolearn/p2data/internal/config"
	"github.com/letsgolearn/p2data/internal/pfilter"
	"github.com/letsgolearn/p2data/internal/redact"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	clf, err := newClassifier(cfg, log)
	if err != nil {
		log.Error("load classifier", "err", err)
		os.Exit(1)
	}
	defer clf.Close()

	srv := api.New(api.Options{
		Classifier:       clf,
		Redactor:         redact.New(cfg.MaskString, cfg.HashSecret),
		APIKeys:          cfg.APIKeys,
		MaxBodyBytes:     cfg.MaxBodyBytes,
		DefaultThreshold: cfg.DefaultThreshold,
		Ready:            func() bool { return true },
		Logger:           log,
	})

	httpServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

// newClassifier loads the native engine, or the regex Fake when
// REDACTOR_USE_FAKE=1 (for local development without a model).
func newClassifier(cfg *config.Config, log *slog.Logger) (pfilter.Classifier, error) {
	if os.Getenv("REDACTOR_USE_FAKE") == "1" {
		log.Warn("using FAKE classifier (regex-based); not for production")
		return pfilter.NewFake("Jane Doe", "John Smith"), nil
	}
	if cfg.ModelPath == "" {
		return nil, errors.New("REDACTOR_MODEL_PATH is required (or set REDACTOR_USE_FAKE=1)")
	}
	log.Info("loading model", "path", cfg.ModelPath, "device", cfg.Device, "pool", cfg.PoolSize)
	return pfilter.Load(pfilter.LoadOptions{
		ModelPath: cfg.ModelPath,
		Device:    cfg.Device,
		NThreads:  cfg.NThreads,
		Window:    cfg.Window,
		PoolSize:  cfg.PoolSize,
	})
}
