package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appauth "easychat/internal/app/auth"
	appchat "easychat/internal/app/chat"
	authinfra "easychat/internal/infrastructure/auth"
	mongoinfra "easychat/internal/infrastructure/mongo"
	httpiface "easychat/internal/interfaces/http"
	"easychat/internal/interfaces/ws"
	"easychat/internal/platform/config"
	"easychat/internal/platform/logger"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoClient interface {
	Database(name string, opts ...*options.DatabaseOptions) *mongo.Database
	Disconnect(ctx context.Context) error
}

type wsManager interface {
	Shutdown()
}

type httpServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
	Close() error
}

type dependencies struct {
	loadConfig    func() (config.Config, error)
	newLogger     func() *slog.Logger
	notifyContext func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc)
	connectMongo  func(ctx context.Context, uri string) (mongoClient, error)
	ensureIndexes func(ctx context.Context, db *mongo.Database) error
	buildRouter   func(cfg config.Config, db *mongo.Database, log *slog.Logger) (http.Handler, wsManager, error)
	newHTTPServer func(port string, handler http.Handler) httpServer
}

func defaultDependencies() dependencies {
	return dependencies{
		loadConfig:    config.Load,
		newLogger:     logger.New,
		notifyContext: signal.NotifyContext,
		connectMongo: func(ctx context.Context, uri string) (mongoClient, error) {
			return mongoinfra.Connect(ctx, uri)
		},
		ensureIndexes: mongoinfra.EnsureIndexes,
		buildRouter:   buildRouter,
		newHTTPServer: newStdHTTPServer,
	}
}

func Run() error {
	return runWithDependencies(defaultDependencies())
}

func runWithDependencies(deps dependencies) error {
	cfg, err := deps.loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := deps.newLogger()
	ctx, stop := deps.notifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mongoClient, err := deps.connectMongo(ctx, cfg.MongoURI)
	if err != nil {
		log.Error("failed to connect to MongoDB", "error", err)
		return fmt.Errorf("connect mongo: %w", err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(disconnectCtx); err != nil {
			log.Warn("failed to disconnect MongoDB", "error", err)
		}
	}()

	db := mongoClient.Database(mongoinfra.DatabaseNameFromURI(cfg.MongoURI))
	if err := deps.ensureIndexes(ctx, db); err != nil {
		log.Error("failed to ensure MongoDB indexes", "error", err)
		return fmt.Errorf("ensure indexes: %w", err)
	}

	router, wsMgr, err := deps.buildRouter(cfg, db, log)
	if err != nil {
		log.Error("failed to build router", "error", err)
		return fmt.Errorf("build router: %w", err)
	}

	server := deps.newHTTPServer(cfg.ServerPort, router)
	serveErrCh := make(chan error, 1)
	go func() {
		log.Info("server starting", slog.String("port", cfg.ServerPort))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			serveErrCh <- err
			stop()
			return
		}
		serveErrCh <- nil
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsMgr.Shutdown()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		log.Error("graceful shutdown failed", "error", shutdownErr)
		if closeErr := server.Close(); closeErr != nil {
			log.Error("forced close failed", "error", closeErr)
		}
	}

	serveErr := <-serveErrCh
	log.Info("server stopped")

	if shutdownErr != nil {
		return shutdownErr
	}
	return serveErr
}

func buildRouter(cfg config.Config, db *mongo.Database, log *slog.Logger) (http.Handler, wsManager, error) {
	var tokenProvider appauth.TokenProvider
	switch cfg.AuthProviderType {
	case "", "jwt":
		tokenProvider = authinfra.NewJWTProvider(cfg.JWTSecret, 24*time.Hour)
	default:
		return nil, nil, fmt.Errorf("unsupported AUTH_PROVIDER_TYPE: %s", cfg.AuthProviderType)
	}

	userRepo := mongoinfra.NewUserRepository(db)
	chatRoomRepo := mongoinfra.NewChatRoomRepository(db)
	messageRepo := mongoinfra.NewMessageRepository(db)

	authService := appauth.NewService(userRepo, tokenProvider)
	chatService := appchat.NewService(userRepo, chatRoomRepo, messageRepo)
	wsMgr := ws.NewManager(log)
	wsHandler := ws.NewHandler(authService, chatService, wsMgr, log)

	authHandler := httpiface.NewAuthHandler(authService)
	chatRoomHandler := httpiface.NewChatRoomHandler(chatService)
	router := httpiface.NewRouter(authService, authHandler, chatRoomHandler, wsHandler)
	return router, wsMgr, nil
}

func newStdHTTPServer(port string, handler http.Handler) httpServer {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
