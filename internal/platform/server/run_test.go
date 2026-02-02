package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"easychat/internal/platform/config"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type fakeMongoClient struct {
	disconnectErr    error
	disconnectCalled bool
	dbName           string
}

func (f *fakeMongoClient) Database(name string, _ ...*options.DatabaseOptions) *mongo.Database {
	f.dbName = name
	return nil
}

func (f *fakeMongoClient) Disconnect(context.Context) error {
	f.disconnectCalled = true
	return f.disconnectErr
}

type fakeWSManager struct {
	shutdownCalled bool
}

func (f *fakeWSManager) Shutdown() {
	f.shutdownCalled = true
}

type fakeHTTPServer struct {
	listenErr      error
	shutdownErr    error
	closeErr       error
	listenCalled   bool
	shutdownCalled bool
	closeCalled    bool
}

func (f *fakeHTTPServer) ListenAndServe() error {
	f.listenCalled = true
	return f.listenErr
}

func (f *fakeHTTPServer) Shutdown(context.Context) error {
	f.shutdownCalled = true
	return f.shutdownErr
}

func (f *fakeHTTPServer) Close() error {
	f.closeCalled = true
	return f.closeErr
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func baseConfig() config.Config {
	return config.Config{
		MongoURI:         "mongodb://localhost:27017/easychat",
		ServerPort:       "8080",
		AuthProviderType: "jwt",
		JWTSecret:        "secret",
	}
}

func TestRunWithDependenciesFailures(t *testing.T) {
	ctxCancelledNotify := func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}

	deps := dependencies{
		loadConfig: func() (config.Config, error) { return config.Config{}, errors.New("bad config") },
	}
	if err := runWithDependencies(deps); err == nil {
		t.Fatalf("expected config error")
	}

	deps = dependencies{
		loadConfig:    func() (config.Config, error) { return baseConfig(), nil },
		newLogger:     noopLogger,
		notifyContext: ctxCancelledNotify,
		connectMongo: func(context.Context, string) (mongoClient, error) {
			return nil, errors.New("connect failed")
		},
	}
	if err := runWithDependencies(deps); err == nil {
		t.Fatalf("expected connect error")
	}
}

func TestRunWithDependenciesEnsureIndexesAndBuildRouterErrors(t *testing.T) {
	ctxCancelledNotify := func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}

	mClient := &fakeMongoClient{}
	deps := dependencies{
		loadConfig:    func() (config.Config, error) { return baseConfig(), nil },
		newLogger:     noopLogger,
		notifyContext: ctxCancelledNotify,
		connectMongo: func(context.Context, string) (mongoClient, error) {
			return mClient, nil
		},
		ensureIndexes: func(context.Context, *mongo.Database) error {
			return errors.New("index failed")
		},
		buildRouter: func(config.Config, *mongo.Database, *slog.Logger) (http.Handler, wsManager, error) {
			return http.NewServeMux(), &fakeWSManager{}, nil
		},
		newHTTPServer: func(string, http.Handler) httpServer {
			return &fakeHTTPServer{}
		},
	}
	if err := runWithDependencies(deps); err == nil {
		t.Fatalf("expected ensure indexes error")
	}
	if !mClient.disconnectCalled {
		t.Fatalf("expected mongo disconnect on error")
	}

	mClient = &fakeMongoClient{}
	deps.ensureIndexes = func(context.Context, *mongo.Database) error { return nil }
	deps.connectMongo = func(context.Context, string) (mongoClient, error) { return mClient, nil }
	deps.buildRouter = func(config.Config, *mongo.Database, *slog.Logger) (http.Handler, wsManager, error) {
		return nil, nil, errors.New("router failed")
	}
	if err := runWithDependencies(deps); err == nil {
		t.Fatalf("expected build router error")
	}
}

func TestRunWithDependenciesServerLifecycle(t *testing.T) {
	newCtxAndCancel := func() (func(context.Context, ...os.Signal) (context.Context, context.CancelFunc), context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		notify := func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
			return ctx, cancel
		}
		return notify, cancel
	}

	notify, cancel := newCtxAndCancel()
	mClient := &fakeMongoClient{}
	wsMgr := &fakeWSManager{}
	httpSrv := &fakeHTTPServer{listenErr: errors.New("listen failed")}
	deps := dependencies{
		loadConfig:    func() (config.Config, error) { return baseConfig(), nil },
		newLogger:     noopLogger,
		notifyContext: notify,
		connectMongo: func(context.Context, string) (mongoClient, error) {
			return mClient, nil
		},
		ensureIndexes: func(context.Context, *mongo.Database) error { return nil },
		buildRouter: func(config.Config, *mongo.Database, *slog.Logger) (http.Handler, wsManager, error) {
			return http.NewServeMux(), wsMgr, nil
		},
		newHTTPServer: func(string, http.Handler) httpServer { return httpSrv },
	}
	err := runWithDependencies(deps)
	if err == nil || err.Error() != "listen failed" {
		t.Fatalf("expected listen error, got %v", err)
	}
	if !httpSrv.listenCalled || !httpSrv.shutdownCalled {
		t.Fatalf("expected listen and shutdown to run")
	}
	if !wsMgr.shutdownCalled {
		t.Fatalf("expected websocket manager shutdown")
	}
	if !mClient.disconnectCalled {
		t.Fatalf("expected mongo disconnect")
	}
	cancel()

	notify, cancel = newCtxAndCancel()
	cancel()
	mClient = &fakeMongoClient{}
	wsMgr = &fakeWSManager{}
	httpSrv = &fakeHTTPServer{listenErr: http.ErrServerClosed, shutdownErr: errors.New("shutdown failed")}
	deps.notifyContext = notify
	deps.connectMongo = func(context.Context, string) (mongoClient, error) { return mClient, nil }
	deps.buildRouter = func(config.Config, *mongo.Database, *slog.Logger) (http.Handler, wsManager, error) {
		return http.NewServeMux(), wsMgr, nil
	}
	deps.newHTTPServer = func(string, http.Handler) httpServer { return httpSrv }

	err = runWithDependencies(deps)
	if err == nil || err.Error() != "shutdown failed" {
		t.Fatalf("expected shutdown error, got %v", err)
	}
	if !httpSrv.closeCalled {
		t.Fatalf("expected forced close after shutdown failure")
	}

	notify, cancel = newCtxAndCancel()
	cancel()
	mClient = &fakeMongoClient{}
	wsMgr = &fakeWSManager{}
	httpSrv = &fakeHTTPServer{listenErr: http.ErrServerClosed}
	deps.notifyContext = notify
	deps.connectMongo = func(context.Context, string) (mongoClient, error) { return mClient, nil }
	deps.newHTTPServer = func(string, http.Handler) httpServer { return httpSrv }

	err = runWithDependencies(deps)
	if err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}

func TestBuildRouterUnsupportedProvider(t *testing.T) {
	_, _, err := buildRouter(config.Config{AuthProviderType: "unsupported", JWTSecret: "secret"}, nil, noopLogger())
	if err == nil {
		t.Fatalf("expected unsupported provider error")
	}
}

func TestBuildRouterSuccess(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		t.Fatalf("failed to create mongo client: %v", err)
	}
	db := client.Database("easychat")

	router, wsMgr, err := buildRouter(config.Config{
		AuthProviderType: "jwt",
		JWTSecret:        "secret",
	}, db, noopLogger())
	if err != nil {
		t.Fatalf("expected buildRouter success, got %v", err)
	}
	if router == nil || wsMgr == nil {
		t.Fatalf("expected router and websocket manager")
	}
}

func TestRunDefaultDependenciesLoadConfigError(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if err := Run(); err == nil {
		t.Fatalf("expected run to fail on missing JWT_SECRET")
	}
}

func TestNewStdHTTPServer(t *testing.T) {
	h := http.NewServeMux()
	srv := newStdHTTPServer("8081", h)
	std, ok := srv.(*http.Server)
	if !ok {
		t.Fatalf("expected *http.Server")
	}
	if std.Addr != ":8081" {
		t.Fatalf("unexpected addr: %s", std.Addr)
	}
	if std.Handler != h {
		t.Fatalf("expected handler wiring")
	}
	if std.ReadHeaderTimeout != 5*time.Second || std.WriteTimeout != 30*time.Second || std.IdleTimeout != 120*time.Second {
		t.Fatalf("unexpected timeout config")
	}
}
