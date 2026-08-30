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

	"github.com/brandopakel/gogifgenerator/internal/config"
	"github.com/brandopakel/gogifgenerator/internal/httpapi"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/provider/wikimedia"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	settings := config.Load()
	localPlanner := planner.Local{}
	var animationPlanner planner.Planner = localPlanner
	paidAIEnabled := settings.PaidAIEnabled && settings.OpenAIAPIKey != ""
	if paidAIEnabled {
		animationPlanner = planner.WithFallback{
			Primary: planner.OpenAI{
				APIKey:  settings.OpenAIAPIKey,
				Model:   settings.OpenAIModel,
				BaseURL: settings.OpenAIBaseURL,
			},
			Fallback: localPlanner,
			OnError: func(err error) {
				logger.Warn("AI planner unavailable; using local planner", "error", err)
			},
		}
	}

	var catalog store.KV = store.NewMemoryKV()
	catalogBackend := "memory"
	var generatedSaver media.GeneratedSaver
	var generatedReader media.GeneratedReader
	if settings.MemKVAddress != "" {
		memkv, err := store.NewMemKV(settings.MemKVAddress)
		if err != nil {
			logger.Error("configure MemKV", "error", err)
			os.Exit(1)
		}
		defer memkv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = memkv.Ping(ctx)
		cancel()
		if err != nil {
			logger.Error("connect to MemKV", "address", settings.MemKVAddress, "error", err)
			os.Exit(1)
		}
		blobs, err := store.NewFileBlobStore(settings.BlobDirectory)
		if err != nil {
			logger.Error("configure blob storage", "error", err)
			os.Exit(1)
		}
		catalog = memkv
		catalogBackend = "memkv"
		library := media.NewLibrary(media.NewRepository(catalog), blobs)
		generatedSaver = library
		generatedReader = library
	}
	commons, err := wikimedia.New(wikimedia.Options{})
	if err != nil {
		logger.Error("configure Wikimedia Commons", "error", err)
		os.Exit(1)
	}
	mediaProviders := []provider.Provider{
		provider.Cached{Next: commons, KV: catalog, TTL: 15 * time.Minute},
	}

	handler := httpapi.New(httpapi.Options{
		Planner:         animationPlanner,
		Logger:          logger,
		AIEnabled:       paidAIEnabled,
		AIModel:         settings.OpenAIModel,
		GiphyAPIKey:     settings.GiphyAPIKey,
		Catalog:         catalog,
		CatalogBackend:  catalogBackend,
		GeneratedSaver:  generatedSaver,
		GeneratedReader: generatedReader,
		Providers:       mediaProviders,
	})
	server := &http.Server{
		Addr:              settings.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()

	logger.Info("GoGIF is ready", "url", httpapi.AddressURL(settings.Address), "ai", paidAIEnabled, "catalog", catalogBackend)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
