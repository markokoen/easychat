package config

import (
	"errors"
	"os"
)

type Config struct {
	MongoURI         string
	NATSURL          string
	ServerPort       string
	AuthProviderType string
	JWTSecret        string
}

func Load() (Config, error) {
	cfg := Config{
		MongoURI:         getEnv("MONGO_URI", "mongodb://localhost:27017/easychat"),
		NATSURL:          getEnv("NATS_URL", "nats://localhost:4222"),
		ServerPort:       getEnv("SERVER_PORT", "8080"),
		AuthProviderType: getEnv("AUTH_PROVIDER_TYPE", "jwt"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
	}

	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	return cfg, nil
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
