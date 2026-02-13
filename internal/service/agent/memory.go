package agent

import (
	"context"
	"encoding/json"

	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
)

const maxMemoryBytes = 4000

// updateMemoryInput is the parsed input for update_memory tool.
type updateMemoryInput struct {
	Content string `json:"content"`
}

// loadMemorySection loads the user's memory document and returns the prompt section.
// Returns empty string if no memory exists or memRepo is nil.
func (s *AgentService) loadMemorySection(ctx context.Context, publicKey string) string {
	if s.memRepo == nil {
		return ""
	}

	mem, err := s.memRepo.GetMemory(ctx, publicKey)
	if err != nil {
		s.logger.WithError(err).Warn("failed to load memory")
		return ""
	}
	if mem == nil {
		return ""
	}

	return BuildMemorySection(mem.Content)
}

// persistMemoryUpdate validates and persists a memory update (fire-and-forget).
// Logs errors/warnings but never returns them — callers should not block on memory failures.
func (s *AgentService) persistMemoryUpdate(ctx context.Context, publicKey string, mu *updateMemoryInput) {
	if mu == nil || s.memRepo == nil {
		return
	}

	if len(mu.Content) > maxMemoryBytes {
		s.logger.WithFields(logrus.Fields{
			"public_key": publicKey,
			"length":     len(mu.Content),
			"max":        maxMemoryBytes,
		}).Warn("memory update rejected: too large")
		return
	}

	if err := s.memRepo.UpsertMemory(ctx, publicKey, mu.Content); err != nil {
		s.logger.WithError(err).Error("failed to update memory")
	} else {
		s.logger.WithFields(logrus.Fields{
			"public_key": publicKey,
			"length":     len(mu.Content),
		}).Debug("memory updated")
	}
}

// memoryTools returns the update_memory tool if memRepo is configured, for appending to ability tool lists.
func (s *AgentService) memoryTools() []anthropic.Tool {
	if s.memRepo == nil {
		return nil
	}
	return []anthropic.Tool{UpdateMemoryTool}
}

// extractMemoryUpdate scans response content blocks for an update_memory tool call.
// Returns nil if not found. Logs and skips malformed input.
func (s *AgentService) extractMemoryUpdate(resp *anthropic.Response) *updateMemoryInput {
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "update_memory" {
			var mu updateMemoryInput
			if err := json.Unmarshal(block.Input, &mu); err != nil {
				s.logger.WithError(err).Warn("failed to unmarshal update_memory")
				continue
			}
			return &mu
		}
	}
	return nil
}
