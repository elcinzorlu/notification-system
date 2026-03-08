package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application.
type Config struct {
	App      AppConfig
	DB       DBConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	Webhook  WebhookConfig
	Worker   WorkerConfig
}

type AppConfig struct {
	Port string
	Env  string
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

type RabbitMQConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	VHost    string
}

type WebhookConfig struct {
	URL string
}

type WorkerConfig struct {
	Concurrency    int
	MaxRetries     int
	RateLimitPerSec int
}

// Load reads configuration from .env file and environment variables.
func Load() (*Config, error) {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Port: getEnv("APP_PORT", "8080"),
			Env:  getEnv("APP_ENV", "development"),
		},
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "notification"),
			Password: getEnv("DB_PASSWORD", "notification_secret"),
			Name:     getEnv("DB_NAME", "notification_system"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		RabbitMQ: RabbitMQConfig{
			Host:     getEnv("RABBITMQ_HOST", "localhost"),
			Port:     getEnv("RABBITMQ_PORT", "5672"),
			User:     getEnv("RABBITMQ_USER", "guest"),
			Password: getEnv("RABBITMQ_PASSWORD", "guest"),
			VHost:    getEnv("RABBITMQ_VHOST", "/"),
		},
		Webhook: WebhookConfig{
			URL: getEnv("WEBHOOK_URL", "https://webhook.site/a2d24e8e-0d36-417d-bc4d-74fe82398181"),
		},
		Worker: WorkerConfig{
			Concurrency:    getEnvInt("WORKER_CONCURRENCY", 10),
			MaxRetries:     getEnvInt("WORKER_MAX_RETRIES", 5),
			RateLimitPerSec: getEnvInt("RATE_LIMIT_PER_SECOND", 100),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// DSN returns the PostgreSQL connection string.
func (c *DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// URL returns the RabbitMQ connection URL.
func (c *RabbitMQConfig) URL() string {
	return fmt.Sprintf(
		"amqp://%s:%s@%s:%s%s",
		c.User, c.Password, c.Host, c.Port, c.VHost,
	)
}

// RedisAddr returns the Redis address.
func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func (c *Config) validate() error {
	if c.DB.Host == "" || c.DB.Name == "" {
		return fmt.Errorf("database host and name are required")
	}
	if c.RabbitMQ.Host == "" {
		return fmt.Errorf("rabbitmq host is required")
	}
	if c.Worker.Concurrency < 1 {
		return fmt.Errorf("worker concurrency must be at least 1")
	}
	if c.Worker.MaxRetries < 1 {
		return fmt.Errorf("worker max retries must be at least 1")
	}
	if c.Worker.RateLimitPerSec < 1 {
		return fmt.Errorf("rate limit per second must be at least 1")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
