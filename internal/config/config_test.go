package config_test

import (
	"os"
	"testing"

	"github.com/elcinzorlu/notification-system/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env to test defaults
	envVars := []string{
		"APP_PORT", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD",
		"DB_NAME", "DB_SSLMODE", "REDIS_HOST", "REDIS_PORT",
		"RABBITMQ_HOST", "RABBITMQ_PORT", "WORKER_CONCURRENCY",
	}
	for _, key := range envVars {
		os.Unsetenv(key)
	}

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "8080", cfg.App.Port)
	assert.Equal(t, "development", cfg.App.Env)
	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, "5432", cfg.DB.Port)
	assert.Equal(t, "localhost", cfg.Redis.Host)
	assert.Equal(t, "6379", cfg.Redis.Port)
	assert.Equal(t, "localhost", cfg.RabbitMQ.Host)
	assert.Equal(t, "5672", cfg.RabbitMQ.Port)
	assert.Equal(t, 10, cfg.Worker.Concurrency)
	assert.Equal(t, 5, cfg.Worker.MaxRetries)
	assert.Equal(t, 100, cfg.Worker.RateLimitPerSec)
}

func TestLoad_CustomValues(t *testing.T) {
	os.Setenv("APP_PORT", "9090")
	os.Setenv("DB_HOST", "db.example.com")
	os.Setenv("WORKER_CONCURRENCY", "20")
	defer func() {
		os.Unsetenv("APP_PORT")
		os.Unsetenv("DB_HOST")
		os.Unsetenv("WORKER_CONCURRENCY")
	}()

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "9090", cfg.App.Port)
	assert.Equal(t, "db.example.com", cfg.DB.Host)
	assert.Equal(t, 20, cfg.Worker.Concurrency)
}

func TestDBConfig_DSN(t *testing.T) {
	cfg := config.DBConfig{
		Host:     "localhost",
		Port:     "5432",
		User:     "user",
		Password: "pass",
		Name:     "testdb",
		SSLMode:  "disable",
	}

	expected := "host=localhost port=5432 user=user password=pass dbname=testdb sslmode=disable"
	assert.Equal(t, expected, cfg.DSN())
}

func TestRabbitMQConfig_URL(t *testing.T) {
	cfg := config.RabbitMQConfig{
		Host:     "localhost",
		Port:     "5672",
		User:     "guest",
		Password: "guest",
		VHost:    "/",
	}

	expected := "amqp://guest:guest@localhost:5672/"
	assert.Equal(t, expected, cfg.URL())
}

func TestRedisConfig_Addr(t *testing.T) {
	cfg := config.RedisConfig{
		Host: "localhost",
		Port: "6379",
	}

	assert.Equal(t, "localhost:6379", cfg.Addr())
}
