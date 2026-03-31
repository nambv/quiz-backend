package metrics

import "github.com/prometheus/client_golang/prometheus"

// AI-ASSISTED: Claude Code — Prometheus metrics for quiz observability
// Verification: metrics match SLIs defined in SYSTEM_DESIGN.md

var (
	// WebSocket connection metrics
	WSConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "quiz_websocket_connections_active",
		Help: "Number of active WebSocket connections.",
	})
	WSConnectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "quiz_websocket_connections_total",
		Help: "Total number of WebSocket connections over lifetime.",
	})

	// Message throughput
	MessagesReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quiz_messages_received_total",
		Help: "Total inbound WebSocket messages by type.",
	}, []string{"type"})
	MessagesBroadcast = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quiz_messages_broadcast_total",
		Help: "Total outbound broadcast messages by type.",
	}, []string{"type"})

	// Answer processing latency (SLI: p99 < 200ms)
	AnswerDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "quiz_answer_processing_duration_seconds",
		Help:    "Time to process an answer submission.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1},
	})

	// Leaderboard update latency (SLI: p99 < 200ms)
	LeaderboardUpdateDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "quiz_leaderboard_update_duration_seconds",
		Help:    "Time to fetch and broadcast leaderboard update.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1},
	})

	// Scoring errors
	ScoringErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quiz_scoring_errors_total",
		Help: "Scoring failures by error type.",
	}, []string{"error_type"})

	// Redis operation latency
	RedisOpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "quiz_redis_operation_duration_seconds",
		Help:    "Redis operation latency by operation.",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
	}, []string{"operation"})
)

func init() {
	prometheus.MustRegister(
		WSConnectionsActive,
		WSConnectionsTotal,
		MessagesReceived,
		MessagesBroadcast,
		AnswerDuration,
		LeaderboardUpdateDuration,
		ScoringErrors,
		RedisOpDuration,
	)
}
