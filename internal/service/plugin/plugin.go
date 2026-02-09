package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/cache/redis"
	"github.com/vultisig/agent-backend/internal/service/agent"
)

const (
	// skillsCacheKey is the Redis key for cached plugin skills.
	skillsCacheKey = "agent:plugin:skills"
	// skillsCacheTTL is how long to cache plugin skills (short to allow dynamic updates).
	skillsCacheTTL = 5 * time.Minute
)

// AvailablePlugin represents a plugin from the verifier API.
type AvailablePlugin struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SkillsMD string `json:"skills_md"`
}

// AvailablePluginsResponse is the verifier API response.
type AvailablePluginsResponse struct {
	Status int `json:"status"`
	Data   struct {
		Plugins []AvailablePlugin `json:"plugins"`
	} `json:"data"`
}

// Service manages plugin discovery and skills.
type Service struct {
	verifierURL string
	redis       *redis.Client
	httpClient  *http.Client
	logger      *logrus.Logger

	// In-memory cache with expiry
	skills      []agent.PluginSkill
	skillsMu    sync.RWMutex
	cacheExpiry time.Time
}

// NewService creates a new plugin service.
func NewService(verifierURL string, redisClient *redis.Client, logger *logrus.Logger) *Service {
	return &Service{
		verifierURL: verifierURL,
		redis:       redisClient,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// GetSkills returns plugin skills, fetching from verifier if cache is expired.
func (s *Service) GetSkills(ctx context.Context) []agent.PluginSkill {
	// Check in-memory cache first
	s.skillsMu.RLock()
	if time.Now().Before(s.cacheExpiry) && len(s.skills) > 0 {
		skills := s.skills
		s.skillsMu.RUnlock()
		return skills
	}
	s.skillsMu.RUnlock()

	// Try Redis cache
	if s.redis != nil {
		cached, err := s.redis.Get(ctx, skillsCacheKey)
		if err == nil && cached != "" {
			var skills []agent.PluginSkill
			if err := json.Unmarshal([]byte(cached), &skills); err == nil && len(skills) > 0 {
				// Update in-memory cache
				s.skillsMu.Lock()
				s.skills = skills
				s.cacheExpiry = time.Now().Add(skillsCacheTTL)
				s.skillsMu.Unlock()
				return skills
			}
		}
	}

	// Fetch from verifier
	skills, err := s.fetchFromVerifier(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("failed to fetch plugins from verifier")
		// Return stale cache if available
		s.skillsMu.RLock()
		stale := s.skills
		s.skillsMu.RUnlock()
		return stale
	}

	// Update caches
	s.skillsMu.Lock()
	s.skills = skills
	s.cacheExpiry = time.Now().Add(skillsCacheTTL)
	s.skillsMu.Unlock()

	if s.redis != nil {
		data, err := json.Marshal(skills)
		if err == nil {
			if err := s.redis.Set(ctx, skillsCacheKey, string(data), skillsCacheTTL); err != nil {
				s.logger.WithError(err).Warn("failed to cache skills in Redis")
			}
		}
	}

	s.logger.WithField("count", len(skills)).Debug("fetched plugin skills from verifier")
	return skills
}

// fetchFromVerifier calls the verifier's /plugins/available endpoint.
func (s *Service) fetchFromVerifier(ctx context.Context) ([]agent.PluginSkill, error) {
	url := fmt.Sprintf("%s/plugins/available", s.verifierURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp AvailablePluginsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert to internal format
	skills := make([]agent.PluginSkill, 0, len(apiResp.Data.Plugins))
	for _, p := range apiResp.Data.Plugins {
		if p.SkillsMD == "" {
			continue
		}
		skills = append(skills, agent.PluginSkill{
			PluginID: p.ID,
			Name:     p.Name,
			Skills:   p.SkillsMD,
		})
	}

	return skills, nil
}

// InvalidateCache clears the skills cache, forcing a fresh fetch on next GetSkills call.
func (s *Service) InvalidateCache(ctx context.Context) {
	s.skillsMu.Lock()
	s.cacheExpiry = time.Time{}
	s.skillsMu.Unlock()

	if s.redis != nil {
		_ = s.redis.Delete(ctx, skillsCacheKey)
	}
}

// GetSkillsForPlugin returns the skills for a specific plugin.
func (s *Service) GetSkillsForPlugin(ctx context.Context, pluginID string) *agent.PluginSkill {
	skills := s.GetSkills(ctx)
	for _, skill := range skills {
		if skill.PluginID == pluginID {
			return &skill
		}
	}
	return nil
}
