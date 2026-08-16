// Package mcp implements the MCP (Model Context Protocol) server for the Synopsis RAG service.
//
// It exposes tools over HTTP using the SSE transport, with a /health endpoint
// for monitoring:
//   - GET  /sse     — MCP SSE transport endpoint (client connection)
//   - POST /message — MCP message endpoint (sessionId query parameter)
//   - GET  /health  — JSON health status
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/search"
	"github.com/devmix/synopsis/internal/logger"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP protocol server with knowledge base dependencies.
type Server struct {
	server   *mcpserver.MCPServer //nolint:stylecheck // mcpserver is the imported package name
	config   config.Config
	db       *database.Database
	searcher search.Searcher
	graph    atomic.Pointer[graph.Graph]
	log      *logger.Logger

	// metrics tracks request statistics for monitoring.
	metrics Metrics

	startAt time.Time // server creation time for uptime calculation
}

// ServerOption configures the MCP server during construction.
type ServerOption func(*Server)

// WithLogger sets the structured logger for the MCP server.
func WithLogger(l *logger.Logger) ServerOption {
	return func(s *Server) { s.log = l }
}

// Metrics holds runtime statistics about MCP tool invocations.
type Metrics struct {
	totalRequests   atomic.Int64
	failedRequests  atomic.Int64
	lastRequestTime atomic.Int64 // unix nanos; 0 means no request has been recorded yet
}

// MetricsSnapshot is an immutable snapshot of current metrics for export.
type MetricsSnapshot struct {
	TotalRequests    int64     `json:"total_requests"`
	FailedRequests   int64     `json:"failed_requests"`
	SuccessRate      float64   `json:"success_rate"`
	LastRequestTime  time.Time `json:"last_request_time,omitempty"`
	TimeSinceRequest string    `json:"time_since_request,omitempty"`
}

// HealthStatus represents the health of the MCP server components.
type HealthStatus struct {
	Status     string            `json:"status"`
	Uptime     string            `json:"uptime,omitempty"`
	Metrics    MetricsSnapshot   `json:"metrics"`
	Components map[string]string `json:"components"`
}

// NewServer creates a new MCP server with all tools registered.
func NewServer(cfg config.Config, db *database.Database, searcher search.Searcher, g *graph.Graph, opts ...ServerOption) (*Server, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if searcher == nil {
		return nil, fmt.Errorf("searcher is required")
	}
	srv := mcpserver.NewMCPServer(
		cfg.Server.Name,
		cfg.Server.Version,
		mcpserver.WithResourceCapabilities(false, false),
	)

	s := &Server{
		server:   srv,
		config:   cfg,
		db:       db,
		searcher: searcher,
		startAt:  time.Now(),
	}
	s.graph.Store(g)

	for _, opt := range opts {
		opt(s)
	}

	// Register middleware for logging.
	srv.Use(s.loggingMiddleware())

	// Register tools.
	s.registerTools()

	return s, nil
}

// Run starts the MCP server over HTTP (SSE transport) and blocks until
// shutdown, handling SIGINT/SIGTERM for graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sseSrv := mcpserver.NewSSEServer(s.server)

	addr := net.JoinHostPort(s.config.Server.Host, strconv.Itoa(s.config.Server.Port))

	mux := http.NewServeMux()
	mux.Handle("/", sseSrv)
	mux.HandleFunc("/health", s.handleHealthHTTP)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	s.log.Infow("MCP server starting", "addr", addr)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http listen: %w", err)
		}
	case <-ctx.Done():
	}

	// Graceful shutdown: close sessions, then the HTTP server.
	sseSrv.CloseSessions()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	s.log.Infow("MCP server stopped gracefully", "total_requests", s.metrics.totalRequests.Load())
	return nil
}

// handleHealthHTTP serves the JSON health status on GET /health.
func (s *Server) handleHealthHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.HealthCheck()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Close releases resources held by the MCP server.
func (s *Server) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// SetGraph swaps the in-memory knowledge graph used by the relation tools.
// Called by the auto-update watcher after a re-index so search stays current.
func (s *Server) SetGraph(g *graph.Graph) {
	s.graph.Store(g)
}

// GetMetrics returns an immutable snapshot of current request metrics.
func (s *Server) GetMetrics() MetricsSnapshot {
	total := s.metrics.totalRequests.Load()
	failed := s.metrics.failedRequests.Load()

	var successRate float64
	if total > 0 {
		successRate = float64(total-failed) / float64(total)
	}

	lastReqNanos := s.metrics.lastRequestTime.Load()
	lastReq := time.Time{}
	if lastReqNanos != 0 {
		lastReq = time.Unix(0, lastReqNanos)
	}
	sinceReq := "N/A"
	if !lastReq.IsZero() {
		sinceReq = time.Since(lastReq).Round(time.Millisecond).String()
	}

	return MetricsSnapshot{
		TotalRequests:    total,
		FailedRequests:   failed,
		SuccessRate:      successRate,
		LastRequestTime:  lastReq,
		TimeSinceRequest: sinceReq,
	}
}

// HealthCheck returns the health status of all server components.
func (s *Server) HealthCheck() HealthStatus {
	status := "healthy"
	components := map[string]string{
		"database": "ok",
		"searcher": "ok",
		"graph":    "ok",
	}

	// Check database connectivity.
	if s.db != nil && s.db.DB() != nil {
		if err := s.db.DB().Ping(); err != nil {
			components["database"] = fmt.Sprintf("error: %v", err)
			status = "degraded"
		}
	} else {
		components["database"] = "not configured"
		status = "unhealthy"
	}

	// Check searcher availability.
	if s.searcher == nil {
		components["searcher"] = "not configured"
		status = "unhealthy"
	}

	// Check graph availability.
	if s.graph.Load() == nil {
		components["graph"] = "not configured"
		status = "unhealthy"
	}

	return HealthStatus{
		Status:     status,
		Uptime:     time.Since(s.startAt).Round(time.Millisecond).String(),
		Metrics:    s.GetMetrics(),
		Components: components,
	}
}
