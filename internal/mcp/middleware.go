package mcp

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// loggingMiddleware returns a middleware that logs MCP tool call requests and responses.
// It records: tool name, arguments, execution duration, and success/failure status.
func (s *Server) loggingMiddleware() mcpserver.ToolHandlerMiddleware {
	return func(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			s.metrics.totalRequests.Add(1)
			s.metrics.lastRequestTime.Store(start.UnixNano())

			toolName := req.Params.Name
			s.log.Debug("MCP tool request", "tool", toolName, "arguments", req.GetArguments())

			result, err := next(ctx, req)

			duration := time.Since(start).Round(time.Millisecond)

			if err != nil {
				s.metrics.failedRequests.Add(1)
				s.log.Error("MCP tool error", "tool", toolName, "duration", duration, "error", err)
			} else {
				s.log.Infow("MCP tool OK", "tool", toolName, "duration", duration)
			}

			return result, err
		}
	}
}
