package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appauth "github.com/markokoen/easychat/internal/app/auth"
	domain "github.com/markokoen/easychat/internal/domain/chat"
)

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
	rooms       map[string]domain.ChatRoom
	createErr   error
	getByIDErr  map[string]error
	addUserErr  error
	removeError error
}

func (f *fakeChatRoomRepo) Create(_ context.Context, room *domain.ChatRoom) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, existing := range f.rooms {
		if existing.Reference == room.Reference {
			return domain.ErrAlreadyExists
		}
	}
	f.rooms[room.ID] = *room
	return nil
}

func (f *fakeChatRoomRepo) GetByID(_ context.Context, id string) (*domain.ChatRoom, error) {
	if err, ok := f.getByIDErr[id]; ok {
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
	if f.removeError != nil {
		return f.removeError
	}
	room, ok := f.rooms[chatRoomID]
	if !ok {
		return domain.ErrNotFound
	}
	users := make([]domain.UserSummary, 0, len(room.Users))
	for _, u := range room.Users {
		if u.ID != userID {
			users = append(users, u)
		}
	}
	room.Users = users
	f.rooms[chatRoomID] = room
	return nil
}

type fakeMessageRepo struct {
	messages            map[string]domain.Message
	createErr           error
	getByIDErr          map[string]error
	upsertDeliveryError error
	upsertReadError     error
}

func (f *fakeMessageRepo) Create(_ context.Context, message *domain.Message) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.messages[message.ID] = *message
	return nil
}

func (f *fakeMessageRepo) GetByID(_ context.Context, id string) (*domain.Message, error) {
	if err, ok := f.getByIDErr[id]; ok {
		return nil, err
	}
	message, ok := f.messages[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &message, nil
}

func (f *fakeMessageRepo) ListByChatRoom(_ context.Context, chatRoomID string, _ int64) ([]domain.Message, error) {
	var out []domain.Message
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
	updated := false
	for i := range message.DeliveryReceipts {
		if message.DeliveryReceipts[i].UserID == receipt.UserID {
			message.DeliveryReceipts[i] = receipt
			updated = true
		}
	}
	if !updated {
		message.DeliveryReceipts = append(message.DeliveryReceipts, receipt)
	}
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
	updated := false
	for i := range message.ReadReceipts {
		if message.ReadReceipts[i].UserID == receipt.UserID {
			message.ReadReceipts[i] = receipt
			updated = true
		}
	}
	if !updated {
		message.ReadReceipts = append(message.ReadReceipts, receipt)
	}
	f.messages[messageID] = message
	return nil
}

func newServiceForTest() (*Service, *fakeUserRepo, *fakeChatRoomRepo, *fakeMessageRepo) {
	userRepo := &fakeUserRepo{users: map[string]domain.User{}, getErrByID: map[string]error{}}
	chatRoomRepo := &fakeChatRoomRepo{rooms: map[string]domain.ChatRoom{}, getByIDErr: map[string]error{}}
	messageRepo := &fakeMessageRepo{messages: map[string]domain.Message{}, getByIDErr: map[string]error{}}
	return NewService(userRepo, chatRoomRepo, messageRepo), userRepo, chatRoomRepo, messageRepo
}

func TestCreateChatRoomValidation(t *testing.T) {
	svc, _, _, _ := newServiceForTest()

	cases := []CreateChatRoomInput{
		{Reference: "", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}},
		{Reference: "room", Users: nil},
		{Reference: "room", Users: []domain.UserSummary{{ID: "", DisplayName: "U1"}}},
		{Reference: "room", Users: []domain.UserSummary{{ID: "u1", DisplayName: ""}}},
	}

	for _, tc := range cases {
		if _, err := svc.CreateChatRoom(context.Background(), tc); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	}
}

func TestCreateChatRoomSuccessDeduplicatesAndSorts(t *testing.T) {
	svc, userRepo, _, _ := newServiceForTest()
	userRepo.users["u2"] = domain.User{ID: "u2", DisplayName: "Old U2", CreatedAt: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}

	room, err := svc.CreateChatRoom(context.Background(), CreateChatRoomInput{
		Reference: "ref-1",
		Users: []domain.UserSummary{
			{ID: "u2", DisplayName: "User 2"},
			{ID: "u1", DisplayName: "User 1"},
			{ID: "u2", DisplayName: "User 2 dup"},
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(room.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(room.Users))
	}
	if room.Users[0].ID != "u1" || room.Users[1].ID != "u2" {
		t.Fatalf("expected sorted users, got %+v", room.Users)
	}
	if !userRepo.users["u2"].CreatedAt.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected existing createdAt to be preserved")
	}
}

func TestCreateChatRoomErrors(t *testing.T) {
	svc, userRepo, chatRooms, _ := newServiceForTest()
	userRepo.getErrByID["u1"] = errors.New("db read failed")
	_, err := svc.CreateChatRoom(context.Background(), CreateChatRoomInput{
		Reference: "ref",
		Users:     []domain.UserSummary{{ID: "u1", DisplayName: "U1"}},
	})
	if err == nil || err.Error() != "db read failed" {
		t.Fatalf("expected user read error, got %v", err)
	}

	svc, userRepo, chatRooms, _ = newServiceForTest()
	userRepo.upsertErr = errors.New("upsert failed")
	_, err = svc.CreateChatRoom(context.Background(), CreateChatRoomInput{
		Reference: "ref",
		Users:     []domain.UserSummary{{ID: "u1", DisplayName: "U1"}},
	})
	if err == nil || err.Error() != "upsert failed" {
		t.Fatalf("expected user upsert error, got %v", err)
	}

	svc, _, chatRooms, _ = newServiceForTest()
	chatRooms.createErr = errors.New("create failed")
	_, err = svc.CreateChatRoom(context.Background(), CreateChatRoomInput{
		Reference: "ref",
		Users:     []domain.UserSummary{{ID: "u1", DisplayName: "U1"}},
	})
	if err == nil || err.Error() != "create failed" {
		t.Fatalf("expected create error, got %v", err)
	}
}

func TestGetChatRoomByIDAndReference(t *testing.T) {
	svc, _, chatRooms, _ := newServiceForTest()
	chatRooms.rooms["r1"] = domain.ChatRoom{ID: "r1", Reference: "ref-1"}

	if _, err := svc.GetChatRoomByID(context.Background(), "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input")
	}
	if _, err := svc.GetChatRoomByReference(context.Background(), " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input")
	}

	room, err := svc.GetChatRoomByID(context.Background(), "r1")
	if err != nil || room.ID != "r1" {
		t.Fatalf("expected room by id, got room=%+v err=%v", room, err)
	}

	room, err = svc.GetChatRoomByReference(context.Background(), "ref-1")
	if err != nil || room.Reference != "ref-1" {
		t.Fatalf("expected room by reference, got room=%+v err=%v", room, err)
	}
}

func TestJoinChatRoom(t *testing.T) {
	svc, _, chatRooms, _ := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}

	if err := svc.JoinChatRoom(context.Background(), "missing", appauth.Claims{UserID: "u2", DisplayName: "U2"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	if err := svc.JoinChatRoom(context.Background(), "room-1", appauth.Claims{UserID: "u1", DisplayName: "U1"}); err != nil {
		t.Fatalf("already member should succeed, got %v", err)
	}

	chatRooms.addUserErr = errors.New("add failed")
	if err := svc.JoinChatRoom(context.Background(), "room-1", appauth.Claims{UserID: "u3", DisplayName: "U3"}); err == nil || err.Error() != "add failed" {
		t.Fatalf("expected add user error, got %v", err)
	}
	chatRooms.addUserErr = nil

	if err := svc.JoinChatRoom(context.Background(), "room-1", appauth.Claims{UserID: "u2", DisplayName: "U2"}); err != nil {
		t.Fatalf("join should succeed, got %v", err)
	}
	room := chatRooms.rooms["room-1"]
	if len(room.Users) != 2 {
		t.Fatalf("expected user to be added")
	}
}

func TestLeaveChatRoom(t *testing.T) {
	svc, _, chatRooms, _ := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}

	if err := svc.LeaveChatRoom(context.Background(), "", "u1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input")
	}
	if err := svc.LeaveChatRoom(context.Background(), "room-1", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input")
	}

	chatRooms.removeError = errors.New("remove failed")
	if err := svc.LeaveChatRoom(context.Background(), "room-1", "u1"); err == nil || err.Error() != "remove failed" {
		t.Fatalf("expected remove error, got %v", err)
	}
	chatRooms.removeError = nil

	if err := svc.LeaveChatRoom(context.Background(), "room-1", "u1"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestSendMessageValidationAndErrors(t *testing.T) {
	svc, _, chatRooms, messages := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}

	_, err := svc.SendMessage(context.Background(), SendMessageInput{ChatRoomID: "missing", Sender: appauth.Claims{UserID: "u1"}, Content: "x"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected room not found")
	}

	_, err = svc.SendMessage(context.Background(), SendMessageInput{ChatRoomID: "room-1", Sender: appauth.Claims{UserID: "u1"}, Content: "   "})
	if !errors.Is(err, ErrMessageEmptyBody) {
		t.Fatalf("expected empty message error")
	}

	_, err = svc.SendMessage(context.Background(), SendMessageInput{ChatRoomID: "room-1", Sender: appauth.Claims{UserID: "u1"}, Content: strings.Repeat("a", 4001)})
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("expected too large message error")
	}

	_, err = svc.SendMessage(context.Background(), SendMessageInput{ChatRoomID: "room-1", Sender: appauth.Claims{UserID: "u2"}, Content: "hello"})
	if !errors.Is(err, ErrNotRoomMember) {
		t.Fatalf("expected not room member error")
	}

	messages.createErr = errors.New("write failed")
	_, err = svc.SendMessage(context.Background(), SendMessageInput{ChatRoomID: "room-1", Sender: appauth.Claims{UserID: "u1", DisplayName: "U1"}, Content: "hello"})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("expected create error, got %v", err)
	}
}

func TestSendMessageSuccess(t *testing.T) {
	svc, _, chatRooms, _ := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}}}

	message, err := svc.SendMessage(context.Background(), SendMessageInput{
		ChatRoomID: "room-1",
		Sender:     appauth.Claims{UserID: "u1", DisplayName: "User 1"},
		Content:    "hello",
	})
	if err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if message.ID == "" || message.ChatRoomID != "room-1" {
		t.Fatalf("unexpected message: %+v", message)
	}
	if len(message.DeliveryReceipts) != 1 || message.DeliveryReceipts[0].Status != domain.DeliveryStatusSent {
		t.Fatalf("expected sender sent receipt")
	}
}

func TestMarkDelivered(t *testing.T) {
	svc, _, chatRooms, messages := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}, {ID: "u2", DisplayName: "U2"}}}
	messages.messages["m1"] = domain.Message{ID: "m1", ChatRoomID: "room-1"}

	messages.getByIDErr["bad"] = errors.New("read failed")
	if _, err := svc.MarkDelivered(context.Background(), "bad", appauth.Claims{UserID: "u1"}); err == nil || err.Error() != "read failed" {
		t.Fatalf("expected message read error, got %v", err)
	}

	chatRooms.getByIDErr["room-1"] = errors.New("room read failed")
	if _, err := svc.MarkDelivered(context.Background(), "m1", appauth.Claims{UserID: "u1"}); err == nil || err.Error() != "room read failed" {
		t.Fatalf("expected room error, got %v", err)
	}
	delete(chatRooms.getByIDErr, "room-1")

	if _, err := svc.MarkDelivered(context.Background(), "m1", appauth.Claims{UserID: "u3"}); !errors.Is(err, ErrNotRoomMember) {
		t.Fatalf("expected membership error, got %v", err)
	}

	messages.upsertDeliveryError = errors.New("update failed")
	if _, err := svc.MarkDelivered(context.Background(), "m1", appauth.Claims{UserID: "u2", DisplayName: "U2"}); err == nil || err.Error() != "update failed" {
		t.Fatalf("expected upsert error, got %v", err)
	}
	messages.upsertDeliveryError = nil

	receipt, err := svc.MarkDelivered(context.Background(), "m1", appauth.Claims{UserID: "u2", DisplayName: "U2"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if receipt.Status != domain.DeliveryStatusDelivered {
		t.Fatalf("expected delivered status, got %s", receipt.Status)
	}
}

func TestMarkRead(t *testing.T) {
	svc, _, chatRooms, messages := newServiceForTest()
	chatRooms.rooms["room-1"] = domain.ChatRoom{ID: "room-1", Users: []domain.UserSummary{{ID: "u1", DisplayName: "U1"}, {ID: "u2", DisplayName: "U2"}}}
	messages.messages["m1"] = domain.Message{ID: "m1", ChatRoomID: "room-1"}

	chatRooms.getByIDErr["room-1"] = errors.New("room read failed")
	if _, err := svc.MarkRead(context.Background(), "room-1", "m1", appauth.Claims{UserID: "u2"}); err == nil || err.Error() != "room read failed" {
		t.Fatalf("expected room read error, got %v", err)
	}
	delete(chatRooms.getByIDErr, "room-1")

	if _, err := svc.MarkRead(context.Background(), "room-1", "m1", appauth.Claims{UserID: "u3"}); !errors.Is(err, ErrNotRoomMember) {
		t.Fatalf("expected not room member, got %v", err)
	}

	messages.getByIDErr["mX"] = errors.New("message read failed")
	if _, err := svc.MarkRead(context.Background(), "room-1", "mX", appauth.Claims{UserID: "u2"}); err == nil || err.Error() != "message read failed" {
		t.Fatalf("expected message read error, got %v", err)
	}

	messages.messages["m2"] = domain.Message{ID: "m2", ChatRoomID: "other"}
	if _, err := svc.MarkRead(context.Background(), "room-1", "m2", appauth.Claims{UserID: "u2"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input for mismatched room, got %v", err)
	}

	messages.upsertReadError = errors.New("upsert failed")
	if _, err := svc.MarkRead(context.Background(), "room-1", "m1", appauth.Claims{UserID: "u2", DisplayName: "U2"}); err == nil || err.Error() != "upsert failed" {
		t.Fatalf("expected upsert error, got %v", err)
	}
	messages.upsertReadError = nil

	receipt, err := svc.MarkRead(context.Background(), "room-1", "m1", appauth.Claims{UserID: "u2", DisplayName: "U2"})
	if err != nil {
		t.Fatalf("mark read failed: %v", err)
	}
	if receipt.UserID != "u2" {
		t.Fatalf("expected receipt user u2")
	}
}
