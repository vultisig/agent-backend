package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration for the agent-backend service.
type Config struct {
	LogFormat string `envconfig:"LOG_FORMAT" default:"json"`
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Anthropic AnthropicConfig
	Verifier  VerifierConfig
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host      string `envconfig:"SERVER_HOST" default:"0.0.0.0"`
	Port      string `envconfig:"SERVER_PORT" default:"8080"`
	JWTSecret string `envconfig:"JWT_SECRET" required:"true"`
}

// DatabaseConfig holds PostgreSQL configuration.
type DatabaseConfig struct {
	DSN string `envconfig:"DATABASE_DSN" required:"true"`
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	URI string `envconfig:"REDIS_URI" required:"true"`
}

// AnthropicConfig holds Anthropic Claude API configuration.
type AnthropicConfig struct {
	APIKey string `envconfig:"ANTHROPIC_API_KEY" required:"true"`
	Model  string `envconfig:"ANTHROPIC_MODEL" default:"claude-sonnet-4-20250514"`
}

// TODO: Add WhisperConfig for OpenAI Whisper voice transcription support.

// VerifierConfig holds verifier service configuration.
type VerifierConfig struct {
	URL string `envconfig:"VERIFIER_URL" required:"true"`
}

// TODO: Add MetricsConfig for Prometheus metrics when metrics are implemented.

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks configuration for logical errors beyond required fields.
func (c *Config) Validate() error {
	// Ensure port defaults are set (defensive, envconfig should handle this)
	if c.Server.Port == "" {
		c.Server.Port = "8080"
	}
	// Add additional validation as needed (e.g., URL format, port ranges)
	return nil
}
