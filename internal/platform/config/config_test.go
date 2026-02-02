package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("NATS_URL", "")
	t.Setenv("SERVER_PORT", "")
	t.Setenv("AUTH_PROVIDER_TYPE", "")
	t.Setenv("JWT_SECRET", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MongoURI != "mongodb://localhost:27017/easychat" {
		t.Fatalf("unexpected default mongo uri: %s", cfg.MongoURI)
	}
	if cfg.NATSURL != "nats://localhost:4222" {
		t.Fatalf("unexpected default nats url: %s", cfg.NATSURL)
	}
	if cfg.ServerPort != "8080" {
		t.Fatalf("unexpected default server port: %s", cfg.ServerPort)
	}
	if cfg.AuthProviderType != "jwt" {
		t.Fatalf("unexpected auth provider type: %s", cfg.AuthProviderType)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://db:27017/custom")
	t.Setenv("NATS_URL", "nats://nats:4222")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("AUTH_PROVIDER_TYPE", "jwt")
	t.Setenv("JWT_SECRET", "abc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.MongoURI != "mongodb://db:27017/custom" ||
		cfg.NATSURL != "nats://nats:4222" ||
		cfg.ServerPort != "9090" ||
		cfg.JWTSecret != "abc" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadMissingJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetEnvFallback(t *testing.T) {
	t.Setenv("X_KEY", "")
	if got := getEnv("X_KEY", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
	t.Setenv("X_KEY", "value")
	if got := getEnv("X_KEY", "fallback"); got != "value" {
		t.Fatalf("expected env value, got %q", got)
	}
}
