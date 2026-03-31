package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nambuivu/quiz-server/internal/config"
	"github.com/nambuivu/quiz-server/internal/handler"
	"github.com/nambuivu/quiz-server/internal/hub"
	_ "github.com/nambuivu/quiz-server/internal/metrics" // register Prometheus metrics
	"github.com/nambuivu/quiz-server/internal/quiz"
	"github.com/nambuivu/quiz-server/internal/repository"
	"github.com/nambuivu/quiz-server/internal/scoring"
	"github.com/nambuivu/quiz-server/pkg/middleware"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Structured JSON logging
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})

	// Choose leaderboard backend: Redis if available, in-memory fallback
	var lb scoring.Leaderboard
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Warn("redis unavailable, using in-memory leaderboard", "addr", cfg.RedisAddr, "error", err)
		lb = scoring.NewMemoryLeaderboard()
		redisClient = nil
	} else {
		log.Info("connected to redis", "addr", cfg.RedisAddr)
		lb = scoring.NewRedisLeaderboard(redisClient)
	}

	// Initialize PostgreSQL if configured
	var pgPool *pgxpool.Pool
	var deps *quiz.Deps
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Warn("postgres unavailable, using mock quiz data", "error", err)
		} else if err := pool.Ping(ctx); err != nil {
			log.Warn("postgres ping failed, using mock quiz data", "error", err)
			pool.Close()
		} else {
			pgPool = pool
			repo := repository.NewPostgres(pool)
			deps = &quiz.Deps{
				QuizRepo:        repo,
				SessionRepo:     repo,
				AnswerRepo:      repo,
				LeaderboardRepo: repo,
			}
			log.Info("connected to postgresql", "url", cfg.DatabaseURL)
		}
	}

	// Wire up dependencies
	h := hub.New(log)
	scorer := scoring.New(log, lb)
	quizSvc := quiz.New(log, h, scorer, deps)
	hdl := handler.New(log, h, quizSvc, scorer, redisClient, pgPool)

	// Register routes
	mux := http.NewServeMux()
	hdl.RegisterRoutes(mux)

	// Apply middleware chain
	var httpHandler http.Handler = mux
	httpHandler = middleware.Logging(log, httpHandler)
	httpHandler = middleware.Recovery(log, httpHandler)
	httpHandler = middleware.CORS(httpHandler)

	server := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      httpHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Info("server starting", "addr", cfg.ServerAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	log.Info("shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", "error", err)
	}

	if redisClient != nil {
		_ = redisClient.Close()
	}
	if pgPool != nil {
		pgPool.Close()
	}

	log.Info("server stopped")
}
