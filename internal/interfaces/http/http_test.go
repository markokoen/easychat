package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	appauth "github.com/markokoen/easychat/internal/app/auth"
	appchat "github.com/markokoen/easychat/internal/app/chat"
	domain "github.com/markokoen/easychat/internal/domain/chat"
	wsiface "github.com/markokoen/easychat/internal/interfaces/ws"

	"github.com/gorilla/mux"
)

type fakeTokenProvider struct {
	generateErr error
	parseErr    error
}

func (f *fakeTokenProvider) GenerateToken(_ context.Context, claims appauth.Claims) (string, error) {
	if f.generateErr != nil {
		return "", f.generateErr
	}
	return "token-" + claims.UserID, nil
}

func (f *fakeTokenProvider) ParseToken(_ context.Context, token string) (appauth.Claims, error) {
	if f.parseErr != nil {
		return appauth.Claims{}, f.parseErr
	}
	return appauth.Claims{UserID: token, DisplayName: "User " + token}, nil
}

type fakeUserRepo struct {
	users      map[string]domain.User
	getErrByID map[string]error
	upsertErr  error
}

func (f *fakeUserRepo) GetByID(_ context.Context, id string) (*domain.User, error) {
	if err, ok := f.getErrByID[id]; ok {
		return nil, err
	}
	if user, ok := f.users[id]; ok {
		return &user, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUserRepo) Upsert(_ context.Context, user *domain.User) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.users[user.ID] = *user
	return nil
}

type fakeChatRoomRepo struct {
	rooms      map[string]domain.ChatRoom
	createErr  error
	getErrByID map[string]error
}

func (f *fakeChatRoomRepo) Create(_ context.Context, chatRoom *domain.ChatRoom) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, existing := range f.rooms {
		if existing.Reference == chatRoom.Reference {
			return domain.ErrAlreadyExists
		}
	}
	f.rooms[chatRoom.ID] = *chatRoom
	return nil
}

func (f *fakeChatRoomRepo) GetByID(_ context.Context, id string) (*domain.ChatRoom, error) {
	if err, ok := f.getErrByID[id]; ok {
		return nil, err
	}
	if room, ok := f.rooms[id]; ok {
		return &room, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeChatRoomRepo) GetByReference(_ context.Context, reference string) (*domain.ChatRoom, error) {
	for _, room := range f.rooms {
		if room.Reference == reference {
			return &room, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeChatRoomRepo) AddUser(_ context.Context, chatRoomID string, user domain.UserSummary) error {
	room, ok := f.rooms[chatRoomID]
	if !ok {
		return domain.ErrNotFound
	}
	room.Users = append(room.Users, user)
	f.rooms[chatRoomID] = room
	return nil
}

func (f *fakeChatRoomRepo) RemoveUser(_ context.Context, chatRoomID string, userID string) error {
	room, ok := f.rooms[chatRoomID]
	if !ok {
		return domain.ErrNotFound
	}
	users := make([]domain.UserSummary, 0, len(room.Users))
	for _, user := range room.Users {
		if user.ID != userID {
			users = append(users, user)
		}
	}
	room.Users = users
	f.rooms[chatRoomID] = room
	return nil
}

type fakeMessageRepo struct{}

func (fakeMessageRepo) Create(_ context.Context, _ *domain.Message) error { return nil }
func (fakeMessageRepo) GetByID(_ context.Context, _ string) (*domain.Message, error) {
	return nil, domain.ErrNotFound
}
func (fakeMessageRepo) ListByChatRoom(_ context.Context, _ string, _ int64) ([]domain.Message, error) {
	return nil, nil
}
func (fakeMessageRepo) UpsertDeliveryReceipt(_ context.Context, _ string, _ domain.DeliveryReceipt) error {
	return nil
}
func (fakeMessageRepo) UpsertReadReceipt(_ context.Context, _ string, _ domain.ReadReceipt) error {
	return nil
}

func newAuthServiceForHTTPTests(userRepo *fakeUserRepo, tokenProvider *fakeTokenProvider) *appauth.Service {
	return appauth.NewService(userRepo, tokenProvider)
}

func newChatServiceForHTTPTests(userRepo *fakeUserRepo, chatRepo *fakeChatRoomRepo) *appchat.Service {
	return appchat.NewService(userRepo, chatRepo, fakeMessageRepo{})
}

func TestWriteHelpers(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"ok": "1"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("unexpected content type: %s", ct)
	}

	rr = httptest.NewRecorder()
	writeError(rr, http.StatusBadRequest, "bad request")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var out map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out["message"] != "bad request" {
		t.Fatalf("unexpected body: %+v", out)
	}
}

func TestContextHelpers(t *testing.T) {
	claims := appauth.Claims{UserID: "u1"}
	ctx := withClaims(context.Background(), claims)
	got, ok := ClaimsFromContext(ctx)
	if !ok || got.UserID != "u1" {
		t.Fatalf("expected claims in context")
	}
	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Fatalf("expected no claims")
	}
}

func TestAuthMiddleware(t *testing.T) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}, getErrByID: map[string]error{}}
	tokens := &fakeTokenProvider{}
	authService := newAuthServiceForHTTPTests(userRepo, tokens)

	protected := AuthMiddleware(authService)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.UserID == "" {
			t.Fatalf("expected claims in context")
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	tokens.parseErr = errors.New("bad token")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer abc")
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rr.Code)
	}

	tokens.parseErr = nil
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer abc")
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthHandlerLogin(t *testing.T) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}, getErrByID: map[string]error{}}
	h := NewAuthHandler(newAuthServiceForHTTPTests(userRepo, &fakeTokenProvider{}))

	rr := httptest.NewRecorder()
	h.Login(rr, httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.Login(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("{")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.Login(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"displayName":"   "}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	userRepo.getErrByID["u1"] = errors.New("db failed")
	rr = httptest.NewRecorder()
	h.Login(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"userId":"u1","displayName":"Name"}`)))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	delete(userRepo.getErrByID, "u1")

	rr = httptest.NewRecorder()
	h.Login(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"displayName":"Name"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestChatRoomHandlerEndpoints(t *testing.T) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}, getErrByID: map[string]error{}}
	chatRepo := &fakeChatRoomRepo{rooms: map[string]domain.ChatRoom{}, getErrByID: map[string]error{}}
	chatSvc := newChatServiceForHTTPTests(userRepo, chatRepo)
	h := NewChatRoomHandler(chatSvc)

	rr := httptest.NewRecorder()
	h.Create(rr, httptest.NewRequest(http.MethodPost, "/api/v1/chatrooms", bytes.NewBufferString("{")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.Create(rr, httptest.NewRequest(http.MethodPost, "/api/v1/chatrooms", bytes.NewBufferString(`{"reference":"","users":[]}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	chatRepo.rooms["existing"] = domain.ChatRoom{ID: "existing", Reference: "ref-dup", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}
	rr = httptest.NewRecorder()
	h.Create(rr, httptest.NewRequest(http.MethodPost, "/api/v1/chatrooms", bytes.NewBufferString(`{"reference":"ref-dup","users":[{"id":"u1","displayName":"U1"}]}`)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	chatRepo.createErr = errors.New("db down")
	rr = httptest.NewRecorder()
	h.Create(rr, httptest.NewRequest(http.MethodPost, "/api/v1/chatrooms", bytes.NewBufferString(`{"reference":"ref-1","users":[{"id":"u1","displayName":"U1"}]}`)))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	chatRepo.createErr = nil

	rr = httptest.NewRecorder()
	h.Create(rr, httptest.NewRequest(http.MethodPost, "/api/v1/chatrooms", bytes.NewBufferString(`{"reference":"ref-2","users":[{"id":"u1","displayName":"U1"}]}`)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	roomID := ""
	for id, room := range chatRepo.rooms {
		if room.Reference == "ref-2" {
			roomID = id
		}
	}
	if roomID == "" {
		t.Fatalf("expected created room")
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/bad", nil)
	req = setMuxVars(req, map[string]string{"chatRoomId": " "})
	h.GetByID(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/missing", nil)
	req = setMuxVars(req, map[string]string{"chatRoomId": "missing"})
	h.GetByID(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	chatRepo.getErrByID[roomID] = errors.New("db failed")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/"+roomID, nil)
	req = setMuxVars(req, map[string]string{"chatRoomId": roomID})
	h.GetByID(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	delete(chatRepo.getErrByID, roomID)

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/"+roomID, nil)
	req = setMuxVars(req, map[string]string{"chatRoomId": roomID})
	h.GetByID(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/reference/space", nil)
	req = setMuxVars(req, map[string]string{"reference": " "})
	h.GetByReference(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/reference/missing", nil)
	req = setMuxVars(req, map[string]string{"reference": "missing"})
	h.GetByReference(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/chatrooms/reference/ref-2", nil)
	req = setMuxVars(req, map[string]string{"reference": "ref-2"})
	h.GetByReference(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRouterRoutes(t *testing.T) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}, getErrByID: map[string]error{}}
	tokens := &fakeTokenProvider{}
	authSvc := newAuthServiceForHTTPTests(userRepo, tokens)
	chatSvc := newChatServiceForHTTPTests(userRepo, &fakeChatRoomRepo{rooms: map[string]domain.ChatRoom{}, getErrByID: map[string]error{}})
	manager := wsiface.NewManager(newNoopLogger())
	wsHandler := wsiface.NewHandler(authSvc, chatSvc, manager, newNoopLogger())

	router := NewRouter(authSvc, NewAuthHandler(authSvc), NewChatRoomHandler(chatSvc), wsHandler)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil))
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound && rr.Code != http.StatusMovedPermanently {
		t.Fatalf("unexpected swagger status: %d", rr.Code)
	}
}

func setMuxVars(req *http.Request, vars map[string]string) *http.Request {
	return mux.SetURLVars(req, vars)
}

func newNoopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
