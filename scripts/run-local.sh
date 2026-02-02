#!/usr/bin/env bash
set -euo pipefail

export MONGO_URI="${MONGO_URI:-mongodb://localhost:27017/easychat}"
export SERVER_PORT="${SERVER_PORT:-8080}"
export AUTH_PROVIDER_TYPE="${AUTH_PROVIDER_TYPE:-jwt}"
export JWT_SECRET="${JWT_SECRET:-dev-secret}"

go run ./cmd/server
