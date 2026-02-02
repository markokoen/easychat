package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	appauth "easychat/internal/app/auth"
	appchat "easychat/internal/app/chat"
	domain "easychat/internal/domain/chat"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type fakeTokenProvider struct {
	parseErr error
}

func (f *fakeTokenProvider) GenerateToken(_ context.Context, claims appauth.Claims) (string, error) {
	return "token-" + claims.UserID, nil
}

func (f *fakeTokenProvider) ParseToken(_ context.Context, token string) (appauth.Claims, error) {
	if f.parseErr != nil {
		return appauth.Claims{}, f.parseErr
	}
	return appauth.Claims{UserID: token, DisplayName: "name-" + token}, nil
}

type fakeUserRepo struct {
	users map[string]domain.User
}

func (f *fakeUserRepo) GetByID(_ context.Context, id string) (*domain.User, error) {
	if user, ok := f.users[id]; ok {
		return &user, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUserRepo) Upsert(_ context.Context, user *domain.User) error {
	f.users[user.ID] = *user
	return nil
}

type fakeChatRoomRepo struct {
	rooms      map[string]domain.ChatRoom
	getErrByID map[string]error
	addUserErr error
	removeErr  error
}

func (f *fakeChatRoomRepo) Create(_ context.Context, chatRoom *domain.ChatRoom) error {
	f.rooms[chatRoom.ID] = *chatRoom
	return nil
}

func (f *fakeChatRoomRepo) GetByID(_ context.Context, id string) (*domain.ChatRoom, error) {
	if err, ok := f.getErrByID[id]; ok {
		return nil, err
	}
	room, ok := f.rooms[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &room, nil
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
	if f.addUserErr != nil {
		return f.addUserErr
	}
	room, ok := f.rooms[chatRoomID]
	if !ok {
		return domain.ErrNotFound
	}
	for _, existing := range room.Users {
		if existing.ID == user.ID {
			return nil
		}
	}
	room.Users = append(room.Users, user)
	f.rooms[chatRoomID] = room
	return nil
}

func (f *fakeChatRoomRepo) RemoveUser(_ context.Context, chatRoomID string, userID string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
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

type fakeMessageRepo struct {
	messages            map[string]domain.Message
	getErrByID          map[string]error
	upsertDeliveryError error
	upsertReadError     error
}

func (f *fakeMessageRepo) Create(_ context.Context, message *domain.Message) error {
	f.messages[message.ID] = *message
	return nil
}

func (f *fakeMessageRepo) GetByID(_ context.Context, id string) (*domain.Message, error) {
	if err, ok := f.getErrByID[id]; ok {
		return nil, err
	}
	message, ok := f.messages[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &message, nil
}

func (f *fakeMessageRepo) ListByChatRoom(_ context.Context, chatRoomID string, _ int64) ([]domain.Message, error) {
	out := make([]domain.Message, 0)
	for _, message := range f.messages {
		if message.ChatRoomID == chatRoomID {
			out = append(out, message)
		}
	}
	return out, nil
}

func (f *fakeMessageRepo) UpsertDeliveryReceipt(_ context.Context, messageID string, receipt domain.DeliveryReceipt) error {
	if f.upsertDeliveryError != nil {
		return f.upsertDeliveryError
	}
	message, ok := f.messages[messageID]
	if !ok {
		return domain.ErrNotFound
	}
	message.DeliveryReceipts = append(message.DeliveryReceipts, receipt)
	f.messages[messageID] = message
	return nil
}

func (f *fakeMessageRepo) UpsertReadReceipt(_ context.Context, messageID string, receipt domain.ReadReceipt) error {
	if f.upsertReadError != nil {
		return f.upsertReadError
	}
	message, ok := f.messages[messageID]
	if !ok {
		return domain.ErrNotFound
	}
	message.ReadReceipts = append(message.ReadReceipts, receipt)
	f.messages[messageID] = message
	return nil
}

type readFrame struct {
	messageType int
	payload     []byte
	err         error
}

type writeFrame struct {
	messageType int
	payload     []byte
}

type fakeConn struct {
	mu             sync.Mutex
	readFrames     []readFrame
	writeFrames    []writeFrame
	writeErrByType map[int]error
	closed         bool
	readLimit      int64
	pongHandler    func(string) error
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.readFrames) == 0 {
		return 0, nil, io.EOF
	}
	frame := f.readFrames[0]
	f.readFrames = f.readFrames[1:]
	return frame.messageType, frame.payload, frame.err
}

func (f *fakeConn) WriteMessage(messageType int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeFrames = append(f.writeFrames, writeFrame{messageType: messageType, payload: append([]byte(nil), data...)})
	if err, ok := f.writeErrByType[messageType]; ok {
		return err
	}
	return nil
}

func (f *fakeConn) SetReadLimit(limit int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readLimit = limit
}

func (f *fakeConn) SetReadDeadline(time.Time) error { return nil }
func (f *fakeConn) SetPongHandler(h func(string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pongHandler = h
}
func (f *fakeConn) SetWriteDeadline(time.Time) error { return nil }

func newNoopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newServicesForWSTest() (*appauth.Service, *appchat.Service, *fakeChatRoomRepo, *fakeMessageRepo) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}}
	chatRepo := &fakeChatRoomRepo{rooms: map[string]domain.ChatRoom{}, getErrByID: map[string]error{}}
	messageRepo := &fakeMessageRepo{messages: map[string]domain.Message{}, getErrByID: map[string]error{}}
	authService := appauth.NewService(userRepo, &fakeTokenProvider{})
	chatService := appchat.NewService(userRepo, chatRepo, messageRepo)
	return authService, chatService, chatRepo, messageRepo
}

func TestManagerCoreFlow(t *testing.T) {
	m := NewManager(newNoopLogger())
	c1, replaced := m.Register("room-1", appauth.Claims{UserID: "u1"}, &fakeConn{})
	if replaced != nil {
		t.Fatalf("did not expect replaced client")
	}
	c2, replaced := m.Register("room-1", appauth.Claims{UserID: "u1"}, &fakeConn{})
	if replaced != c1 {
		t.Fatalf("expected first client to be replaced")
	}
	if removed := m.Unregister(c1); removed {
		t.Fatalf("stale client should not remove current one")
	}
	if removed := m.Unregister(c2); !removed {
		t.Fatalf("expected current client to be removed")
	}

	if ids := m.RoomUserIDs("room-1"); len(ids) != 0 {
		t.Fatalf("expected empty room, got %+v", ids)
	}
}

func TestManagerEnqueueBroadcastAndShutdown(t *testing.T) {
	m := NewManager(newNoopLogger())
	conn1 := &fakeConn{}
	conn2 := &fakeConn{}
	c1, _ := m.Register("room-1", appauth.Claims{UserID: "u1"}, conn1)
	c2, _ := m.Register("room-1", appauth.Claims{UserID: "u2"}, conn2)

	if ok := m.Enqueue(c1, Envelope{Type: "ok", Payload: map[string]any{"v": 1}}); !ok {
		t.Fatalf("enqueue should succeed")
	}
	if ok := m.Enqueue(c1, Envelope{Type: "bad", Payload: func() {}}); ok {
		t.Fatalf("marshal error should fail enqueue")
	}

	slow := &Client{
		chatRoomID: "room-1",
		user:       appauth.Claims{UserID: "slow"},
		conn:       &fakeConn{},
		send:       make(chan []byte),
	}
	m.mu.Lock()
	m.rooms["room-1"]["slow"] = slow
	m.mu.Unlock()
	if ok := m.Enqueue(slow, Envelope{Type: "x", Payload: map[string]any{}}); ok {
		t.Fatalf("expected slow client to be dropped")
	}

	m.Broadcast("room-1", Envelope{Type: "broadcast", Payload: map[string]any{}}, "u2")
	if len(c2.send) != 0 {
		t.Fatalf("excluded user should not receive broadcast")
	}
	if len(c1.send) < 2 {
		t.Fatalf("expected u1 to receive broadcast")
	}

	m.SendToUser("room-1", "u2", Envelope{Type: "direct", Payload: map[string]any{"x": 1}})
	if len(c2.send) != 1 {
		t.Fatalf("expected direct message to u2")
	}

	users := m.RoomUsers("room-1")
	if len(users) != 2 {
		t.Fatalf("expected 2 users in room, got %d", len(users))
	}

	m.Shutdown()
	if len(m.rooms) != 0 {
		t.Fatalf("expected rooms map to be reset")
	}
	if !conn1.closed || !conn2.closed {
		t.Fatalf("expected all clients to be closed on shutdown")
	}
}

func TestHandlerHelpers(t *testing.T) {
	authService, chatService, _, _ := newServicesForWSTest()
	h := NewHandler(authService, chatService, NewManager(newNoopLogger()), newNoopLogger())

	req := httptest.NewRequest(http.MethodGet, "/ws/chatrooms/r1", nil)
	if _, ok := h.extractClaims(req); ok {
		t.Fatalf("expected missing auth to fail")
	}

	req.Header.Set("Authorization", "Bearer u1")
	claims, ok := h.extractClaims(req)
	if !ok || claims.UserID != "u1" {
		t.Fatalf("expected valid claims, got %+v", claims)
	}

	client := &Client{chatRoomID: "r1", user: appauth.Claims{UserID: "u1"}, conn: &fakeConn{}, send: make(chan []byte, 4)}
	h.sendError(client, "boom", "req-1")
	envelopes := drainEnvelopes(client.send)
	if len(envelopes) != 1 || envelopes[0].Type != "error" {
		t.Fatalf("expected one error envelope, got %+v", envelopes)
	}

	rr := httptest.NewRecorder()
	writeHTTPError(rr, http.StatusBadRequest, "nope")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	if msg := mapSendError(appchat.ErrMessageTooLarge); msg != "message content exceeds size limit" {
		t.Fatalf("unexpected message mapping: %s", msg)
	}
}

func TestHandleMessageSendAndRead(t *testing.T) {
	authService, chatService, chatRepo, messageRepo := newServicesForWSTest()
	chatRepo.rooms["room-1"] = domain.ChatRoom{
		ID: "room-1",
		Users: []domain.UserSummary{
			{ID: "u1", DisplayName: "U1"},
			{ID: "u2", DisplayName: "U2"},
		},
	}

	manager := NewManager(newNoopLogger())
	h := NewHandler(authService, chatService, manager, newNoopLogger())
	sender, _ := manager.Register("room-1", appauth.Claims{UserID: "u1", DisplayName: "U1"}, &fakeConn{})
	receiver, _ := manager.Register("room-1", appauth.Claims{UserID: "u2", DisplayName: "U2"}, &fakeConn{})

	h.handleMessageSend(sender, incomingEnvelope{Type: "message.send", RequestID: "req-1", Payload: json.RawMessage(`{"content":"hello"}`)})
	if len(messageRepo.messages) != 1 {
		t.Fatalf("expected message to be persisted")
	}

	senderEvents := envelopeTypes(drainEnvelopes(sender.send))
	receiverEvents := envelopeTypes(drainEnvelopes(receiver.send))
	assertContains(t, senderEvents, "message.created")
	assertContains(t, senderEvents, "message.sent")
	assertContains(t, senderEvents, "message.delivered")
	assertContains(t, receiverEvents, "message.created")
	assertContains(t, receiverEvents, "message.delivered")

	h.handleMessageSend(sender, incomingEnvelope{Type: "message.send", RequestID: "req-2", Payload: json.RawMessage(`{"content":`)})
	errEvents := drainEnvelopes(sender.send)
	if len(errEvents) == 0 || errEvents[0].Type != "error" {
		t.Fatalf("expected error for bad payload")
	}

	h.handleMessageSend(sender, incomingEnvelope{Type: "message.send", RequestID: "req-3", Payload: json.RawMessage(`{"content":"   "}`)})
	errEvents = drainEnvelopes(sender.send)
	if len(errEvents) == 0 || errEvents[0].Type != "error" {
		t.Fatalf("expected mapped send error")
	}

	messageID := ""
	for id := range messageRepo.messages {
		messageID = id
	}
	h.handleMessageRead(sender, incomingEnvelope{Type: "message.read", RequestID: "req-4", Payload: json.RawMessage(`{"messageId":"` + messageID + `"}`)})
	readEvents := envelopeTypes(drainEnvelopes(sender.send))
	assertContains(t, readEvents, "message.read")

	h.handleMessageRead(sender, incomingEnvelope{Type: "message.read", RequestID: "req-5", Payload: json.RawMessage(`{"messageId":`)})
	errEvents = drainEnvelopes(sender.send)
	if len(errEvents) == 0 || errEvents[0].Type != "error" {
		t.Fatalf("expected error for invalid read payload")
	}

	messageRepo.getErrByID["missing"] = domain.ErrNotFound
	h.handleMessageRead(sender, incomingEnvelope{Type: "message.read", RequestID: "req-6", Payload: json.RawMessage(`{"messageId":"missing"}`)})
	errEvents = drainEnvelopes(sender.send)
	if len(errEvents) == 0 || errEvents[0].Type != "error" {
		t.Fatalf("expected mark read failure")
	}
}

func TestCleanupReadPumpWritePump(t *testing.T) {
	authService, chatService, chatRepo, messageRepo := newServicesForWSTest()
	chatRepo.rooms["room-1"] = domain.ChatRoom{
		ID: "room-1",
		Users: []domain.UserSummary{
			{ID: "u1", DisplayName: "U1"},
			{ID: "u2", DisplayName: "U2"},
		},
	}
	messageRepo.messages["m1"] = domain.Message{ID: "m1", ChatRoomID: "room-1", SenderUserID: "u1", SenderUserName: "U1", Content: "hello", CreatedAt: time.Now().UTC()}

	manager := NewManager(newNoopLogger())
	h := NewHandler(authService, chatService, manager, newNoopLogger())
	clientConn := &fakeConn{readFrames: []readFrame{
		{messageType: websocket.TextMessage, payload: []byte("{"), err: nil},
		{messageType: websocket.TextMessage, payload: []byte(`{"type":"unknown","payload":{}}`), err: nil},
		{messageType: websocket.TextMessage, payload: []byte(`{"type":"message.send","requestId":"x1","payload":{"content":"hello"}}`), err: nil},
		{messageType: websocket.TextMessage, payload: []byte(`{"type":"message.read","requestId":"x2","payload":{"messageId":"m1"}}`), err: nil},
		{messageType: 0, payload: nil, err: &websocket.CloseError{Code: websocket.CloseNormalClosure}},
	}}
	otherConn := &fakeConn{}
	client, _ := manager.Register("room-1", appauth.Claims{UserID: "u1", DisplayName: "U1"}, clientConn)
	other, _ := manager.Register("room-1", appauth.Claims{UserID: "u2", DisplayName: "U2"}, otherConn)

	h.readPump(client)
	if !clientConn.closed {
		t.Fatalf("expected readPump cleanup to close conn")
	}
	if ids := manager.RoomUserIDs("room-1"); len(ids) != 1 || ids[0] != "u2" {
		t.Fatalf("expected u1 removed from manager, got %+v", ids)
	}
	leftEvents := envelopeTypes(drainEnvelopes(other.send))
	assertContains(t, leftEvents, "user.left")

	client2 := &Client{chatRoomID: "room-1", user: appauth.Claims{UserID: "writer"}, conn: &fakeConn{}, send: make(chan []byte, 4)}
	done := make(chan struct{})
	go func() {
		h.writePump(client2)
		close(done)
	}()
	client2.send <- []byte(`{"type":"x"}`)
	client2.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writePump did not exit")
	}

	origPingPeriod := pingPeriod
	origWriteWait := writeWait
	pingPeriod = 1 * time.Millisecond
	writeWait = 5 * time.Millisecond
	t.Cleanup(func() {
		pingPeriod = origPingPeriod
		writeWait = origWriteWait
	})

	pingConn := &fakeConn{writeErrByType: map[int]error{websocket.PingMessage: errors.New("stop")}}
	client3 := &Client{chatRoomID: "room-1", user: appauth.Claims{UserID: "pinger"}, conn: pingConn, send: make(chan []byte, 1)}
	h.writePump(client3)
	if countWritesByType(pingConn.writeFrames, websocket.PingMessage) == 0 {
		t.Fatalf("expected at least one ping write")
	}
}

func TestServeWSErrorAndSuccessPaths(t *testing.T) {
	authService, chatService, chatRepo, _ := newServicesForWSTest()
	chatRepo.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}

	manager := NewManager(newNoopLogger())
	h := NewHandler(authService, chatService, manager, newNoopLogger())

	rr := httptest.NewRecorder()
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/room-1", nil), map[string]string{"chatRoomId": "room-1"})
	h.ServeWS(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/room-1", nil), map[string]string{"chatRoomId": " "})
	req.Header.Set("Authorization", "Bearer u1")
	h.ServeWS(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/missing", nil), map[string]string{"chatRoomId": "missing"})
	req.Header.Set("Authorization", "Bearer u1")
	h.ServeWS(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	chatRepo.getErrByID["room-1"] = errors.New("db failed")
	rr = httptest.NewRecorder()
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/room-1", nil), map[string]string{"chatRoomId": "room-1"})
	req.Header.Set("Authorization", "Bearer u1")
	h.ServeWS(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	delete(chatRepo.getErrByID, "room-1")

	h.upgrade = func(http.ResponseWriter, *http.Request) (wsConn, error) {
		return nil, errors.New("upgrade fail")
	}
	rr = httptest.NewRecorder()
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/room-1", nil), map[string]string{"chatRoomId": "room-1"})
	req.Header.Set("Authorization", "Bearer u1")
	h.ServeWS(rr, req)

	oldConn := &fakeConn{}
	_, _ = manager.Register("room-1", appauth.Claims{UserID: "u1", DisplayName: "U1"}, oldConn)
	h.upgrade = func(http.ResponseWriter, *http.Request) (wsConn, error) {
		return &fakeConn{readFrames: []readFrame{{err: io.EOF}}}, nil
	}
	rr = httptest.NewRecorder()
	req = mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/ws/chatrooms/room-1", nil), map[string]string{"chatRoomId": "room-1"})
	req.Header.Set("Authorization", "Bearer u1")
	h.ServeWS(rr, req)

	if !oldConn.closed {
		t.Fatalf("expected replaced connection to be closed")
	}
}

func drainEnvelopes(ch chan []byte) []Envelope {
	out := make([]Envelope, 0, len(ch))
	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				return out
			}
			var env Envelope
			if err := json.Unmarshal(raw, &env); err == nil {
				out = append(out, env)
			}
		default:
			return out
		}
	}
}

func envelopeTypes(envelopes []Envelope) []string {
	types := make([]string, 0, len(envelopes))
	for _, env := range envelopes {
		types = append(types, env.Type)
	}
	sort.Strings(types)
	return types
}

func assertContains(t *testing.T, items []string, target string) {
	t.Helper()
	for _, item := range items {
		if item == target {
			return
		}
	}
	t.Fatalf("expected %q in %+v", target, items)
}

func countWritesByType(frames []writeFrame, messageType int) int {
	count := 0
	for _, frame := range frames {
		if frame.messageType == messageType {
			count++
		}
	}
	return count
}

func TestProtocolEnvelopeJSONRoundTrip(t *testing.T) {
	env := Envelope{Type: "message.send", RequestID: "abc", Payload: map[string]any{"content": "hello"}}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"type":"message.send"`)) {
		t.Fatalf("unexpected json: %s", string(raw))
	}
}
