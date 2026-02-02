package mongo

import (
	"context"
	"errors"
	"testing"
	"time"

	"easychat/internal/domain/chat"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type fakeDatabase struct {
	collections map[string]*fakeCollection
}

func (d fakeDatabase) Collection(name string, _ ...*options.CollectionOptions) collectionAPI {
	return d.collections[name]
}

type fakeCollection struct {
	insertErr        error
	insertCalls      int
	findOneResult    singleResultAPI
	updateByIDErr    error
	updateByIDCalled int
	updateQueue      []updateResponse
	updateCalls      int
	findCursor       cursorAPI
	findErr          error
}

type updateResponse struct {
	result *mongodriver.UpdateResult
	err    error
}

func (c *fakeCollection) InsertOne(context.Context, any, ...*options.InsertOneOptions) (*mongodriver.InsertOneResult, error) {
	c.insertCalls++
	return &mongodriver.InsertOneResult{}, c.insertErr
}

func (c *fakeCollection) FindOne(context.Context, any, ...*options.FindOneOptions) singleResultAPI {
	return c.findOneResult
}

func (c *fakeCollection) UpdateOne(context.Context, any, any, ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	c.updateCalls++
	if c.updateCalls <= len(c.updateQueue) {
		resp := c.updateQueue[c.updateCalls-1]
		return resp.result, resp.err
	}
	return &mongodriver.UpdateResult{}, nil
}

func (c *fakeCollection) UpdateByID(context.Context, any, any, ...*options.UpdateOptions) (*mongodriver.UpdateResult, error) {
	c.updateByIDCalled++
	if c.updateByIDErr != nil {
		return nil, c.updateByIDErr
	}
	return &mongodriver.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, nil
}

func (c *fakeCollection) Find(context.Context, any, ...*options.FindOptions) (cursorAPI, error) {
	if c.findErr != nil {
		return nil, c.findErr
	}
	return c.findCursor, nil
}

type fakeSingleResult struct {
	value any
	err   error
}

func (s fakeSingleResult) Decode(v any) error {
	if s.err != nil {
		return s.err
	}
	raw, err := bson.Marshal(s.value)
	if err != nil {
		return err
	}
	return bson.Unmarshal(raw, v)
}

type fakeCursor struct {
	docs   []any
	idx    int
	curr   any
	decErr error
	err    error
	closed bool
}

func (c *fakeCursor) Next(context.Context) bool {
	if c.idx >= len(c.docs) {
		return false
	}
	c.curr = c.docs[c.idx]
	c.idx++
	return true
}

func (c *fakeCursor) Decode(v any) error {
	if c.decErr != nil {
		return c.decErr
	}
	raw, err := bson.Marshal(c.curr)
	if err != nil {
		return err
	}
	return bson.Unmarshal(raw, v)
}

func (c *fakeCursor) Close(context.Context) error {
	c.closed = true
	return nil
}

func (c *fakeCursor) Err() error {
	return c.err
}

func TestUserRepository(t *testing.T) {
	col := &fakeCollection{}
	repo := NewUserRepositoryWithDatabase(fakeDatabase{collections: map[string]*fakeCollection{"users": col}})

	col.findOneResult = fakeSingleResult{err: mongodriver.ErrNoDocuments}
	if _, err := repo.GetByID(context.Background(), "u1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	col.findOneResult = fakeSingleResult{err: errors.New("read failed")}
	if _, err := repo.GetByID(context.Background(), "u1"); err == nil || err.Error() != "read failed" {
		t.Fatalf("expected read error, got %v", err)
	}

	col.findOneResult = fakeSingleResult{value: chat.User{ID: "u1", DisplayName: "User 1", CreatedAt: time.Now().UTC()}}
	user, err := repo.GetByID(context.Background(), "u1")
	if err != nil || user.ID != "u1" {
		t.Fatalf("expected user, got user=%+v err=%v", user, err)
	}

	col.updateByIDErr = errors.New("write failed")
	if err := repo.Upsert(context.Background(), &chat.User{ID: "u1"}); err == nil || err.Error() != "write failed" {
		t.Fatalf("expected upsert error, got %v", err)
	}

	col.updateByIDErr = nil
	if err := repo.Upsert(context.Background(), &chat.User{ID: "u1"}); err != nil {
		t.Fatalf("expected upsert success, got %v", err)
	}
}

func TestChatRoomRepository(t *testing.T) {
	col := &fakeCollection{}
	repo := NewChatRoomRepositoryWithDatabase(fakeDatabase{collections: map[string]*fakeCollection{"chatrooms": col}})

	col.insertErr = mongodriver.WriteException{WriteErrors: []mongodriver.WriteError{{Code: 11000}}}
	if err := repo.Create(context.Background(), &chat.ChatRoom{ID: "r1"}); !errors.Is(err, chat.ErrAlreadyExists) {
		t.Fatalf("expected already exists, got %v", err)
	}

	col.insertErr = errors.New("insert failed")
	if err := repo.Create(context.Background(), &chat.ChatRoom{ID: "r1"}); err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected insert error, got %v", err)
	}

	col.insertErr = nil
	if err := repo.Create(context.Background(), &chat.ChatRoom{ID: "r1"}); err != nil {
		t.Fatalf("expected create success, got %v", err)
	}

	col.findOneResult = fakeSingleResult{err: mongodriver.ErrNoDocuments}
	if _, err := repo.GetByID(context.Background(), "x"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	col.findOneResult = fakeSingleResult{value: chat.ChatRoom{ID: "r1", Reference: "ref"}}
	if room, err := repo.GetByID(context.Background(), "r1"); err != nil || room.ID != "r1" {
		t.Fatalf("expected room by id, got room=%+v err=%v", room, err)
	}
	if room, err := repo.GetByReference(context.Background(), "ref"); err != nil || room.Reference != "ref" {
		t.Fatalf("expected room by reference, got room=%+v err=%v", room, err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil}}
	col.updateCalls = 0
	if err := repo.AddUser(context.Background(), "r1", chat.UserSummary{ID: "u1"}); err != nil {
		t.Fatalf("expected add user success, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil}}
	col.findOneResult = fakeSingleResult{err: mongodriver.ErrNoDocuments}
	col.updateCalls = 0
	if err := repo.AddUser(context.Background(), "missing", chat.UserSummary{ID: "u1"}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected fallback not found, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: nil, err: errors.New("update failed")}}
	col.updateCalls = 0
	if err := repo.RemoveUser(context.Background(), "r1", "u1"); err == nil || err.Error() != "update failed" {
		t.Fatalf("expected remove error, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil}}
	col.updateCalls = 0
	if err := repo.RemoveUser(context.Background(), "missing", "u1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected remove not found, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil}}
	col.updateCalls = 0
	if err := repo.RemoveUser(context.Background(), "r1", "u1"); err != nil {
		t.Fatalf("expected remove success, got %v", err)
	}
}

func TestMessageRepository(t *testing.T) {
	col := &fakeCollection{}
	repo := NewMessageRepositoryWithDatabase(fakeDatabase{collections: map[string]*fakeCollection{"messages": col}})

	col.insertErr = errors.New("insert failed")
	if err := repo.Create(context.Background(), &chat.Message{ID: "m1"}); err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected create error, got %v", err)
	}
	col.insertErr = nil
	if err := repo.Create(context.Background(), &chat.Message{ID: "m1"}); err != nil {
		t.Fatalf("expected create success, got %v", err)
	}

	col.findOneResult = fakeSingleResult{err: mongodriver.ErrNoDocuments}
	if _, err := repo.GetByID(context.Background(), "x"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	col.findOneResult = fakeSingleResult{value: chat.Message{ID: "m1", ChatRoomID: "r1"}}
	if msg, err := repo.GetByID(context.Background(), "m1"); err != nil || msg.ID != "m1" {
		t.Fatalf("expected message, got msg=%+v err=%v", msg, err)
	}

	col.findErr = errors.New("find failed")
	if _, err := repo.ListByChatRoom(context.Background(), "r1", 0); err == nil || err.Error() != "find failed" {
		t.Fatalf("expected list find error, got %v", err)
	}
	col.findErr = nil

	cursor := &fakeCursor{docs: []any{chat.Message{ID: "m1", ChatRoomID: "r1"}, chat.Message{ID: "m2", ChatRoomID: "r1"}}}
	col.findCursor = cursor
	msgs, err := repo.ListByChatRoom(context.Background(), "r1", 0)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("expected two messages, got len=%d err=%v", len(msgs), err)
	}
	if !cursor.closed {
		t.Fatalf("expected cursor to be closed")
	}

	cursor = &fakeCursor{docs: []any{chat.Message{ID: "m1"}}, decErr: errors.New("decode failed")}
	col.findCursor = cursor
	if _, err := repo.ListByChatRoom(context.Background(), "r1", 10); err == nil || err.Error() != "decode failed" {
		t.Fatalf("expected decode error, got %v", err)
	}

	cursor = &fakeCursor{docs: []any{}, err: errors.New("cursor err")}
	col.findCursor = cursor
	if _, err := repo.ListByChatRoom(context.Background(), "r1", 10); err == nil || err.Error() != "cursor err" {
		t.Fatalf("expected cursor error, got %v", err)
	}

	receipt := chat.DeliveryReceipt{UserID: "u1", UserName: "U1", Status: chat.DeliveryStatusDelivered}
	col.updateQueue = []updateResponse{{result: nil, err: errors.New("update failed")}}
	col.updateCalls = 0
	if err := repo.UpsertDeliveryReceipt(context.Background(), "m1", receipt); err == nil || err.Error() != "update failed" {
		t.Fatalf("expected upsert delivery error, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil}}
	col.updateCalls = 0
	if err := repo.UpsertDeliveryReceipt(context.Background(), "m1", receipt); err != nil {
		t.Fatalf("expected update existing delivery receipt, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: nil, err: errors.New("push failed")},
	}
	col.updateCalls = 0
	if err := repo.UpsertDeliveryReceipt(context.Background(), "m1", receipt); err == nil || err.Error() != "push failed" {
		t.Fatalf("expected delivery push error, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
	}
	col.updateCalls = 0
	if err := repo.UpsertDeliveryReceipt(context.Background(), "missing", receipt); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected delivery not found, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil},
	}
	col.updateCalls = 0
	if err := repo.UpsertDeliveryReceipt(context.Background(), "m1", receipt); err != nil {
		t.Fatalf("expected delivery upsert success, got %v", err)
	}

	readReceipt := chat.ReadReceipt{UserID: "u1", UserName: "U1", ReadAt: time.Now().UTC()}
	col.updateQueue = []updateResponse{{result: nil, err: errors.New("update failed")}}
	col.updateCalls = 0
	if err := repo.UpsertReadReceipt(context.Background(), "m1", readReceipt); err == nil || err.Error() != "update failed" {
		t.Fatalf("expected read update error, got %v", err)
	}

	col.updateQueue = []updateResponse{{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil}}
	col.updateCalls = 0
	if err := repo.UpsertReadReceipt(context.Background(), "m1", readReceipt); err != nil {
		t.Fatalf("expected existing read update success, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: nil, err: errors.New("push failed")},
	}
	col.updateCalls = 0
	if err := repo.UpsertReadReceipt(context.Background(), "m1", readReceipt); err == nil || err.Error() != "push failed" {
		t.Fatalf("expected read push error, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
	}
	col.updateCalls = 0
	if err := repo.UpsertReadReceipt(context.Background(), "missing", readReceipt); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("expected read not found, got %v", err)
	}

	col.updateQueue = []updateResponse{
		{result: &mongodriver.UpdateResult{MatchedCount: 0}, err: nil},
		{result: &mongodriver.UpdateResult{MatchedCount: 1}, err: nil},
	}
	col.updateCalls = 0
	if err := repo.UpsertReadReceipt(context.Background(), "m1", readReceipt); err != nil {
		t.Fatalf("expected read upsert success, got %v", err)
	}
}

func TestConnectHelper(t *testing.T) {
	ctx := context.Background()

	_, err := connect(ctx, "mongodb://localhost:27017", func(context.Context, ...*options.ClientOptions) (*mongodriver.Client, error) {
		return nil, errors.New("connect failed")
	}, func(*mongodriver.Client, context.Context) error {
		return nil
	}, func(*mongodriver.Client, context.Context) error {
		return nil
	})
	if err == nil || err.Error() != "connect failed" {
		t.Fatalf("expected connect error, got %v", err)
	}

	disconnectCalled := false
	_, err = connect(ctx, "mongodb://localhost:27017", func(context.Context, ...*options.ClientOptions) (*mongodriver.Client, error) {
		return &mongodriver.Client{}, nil
	}, func(*mongodriver.Client, context.Context) error {
		return errors.New("ping failed")
	}, func(*mongodriver.Client, context.Context) error {
		disconnectCalled = true
		return nil
	})
	if err == nil || err.Error() != "ping failed" {
		t.Fatalf("expected ping error, got %v", err)
	}
	if !disconnectCalled {
		t.Fatalf("expected disconnect to be called when ping fails")
	}

	client, err := connect(ctx, "mongodb://localhost:27017", func(context.Context, ...*options.ClientOptions) (*mongodriver.Client, error) {
		return &mongodriver.Client{}, nil
	}, func(*mongodriver.Client, context.Context) error {
		return nil
	}, func(*mongodriver.Client, context.Context) error {
		return nil
	})
	if err != nil || client == nil {
		t.Fatalf("expected successful connect helper, got client=%v err=%v", client, err)
	}
}

type fakeIndexDB struct {
	cols map[string]*fakeIndexCollection
}

func (d fakeIndexDB) Collection(name string, _ ...*options.CollectionOptions) indexCollectionAPI {
	return d.cols[name]
}

type fakeIndexCollection struct {
	view *fakeIndexView
}

func (c *fakeIndexCollection) Indexes() indexViewAPI {
	return c.view
}

type fakeIndexView struct {
	createOneErr  error
	createManyErr error
	oneCalls      int
	manyCalls     int
}

func (v *fakeIndexView) CreateOne(context.Context, mongodriver.IndexModel, ...*options.CreateIndexesOptions) (string, error) {
	v.oneCalls++
	return "idx1", v.createOneErr
}

func (v *fakeIndexView) CreateMany(context.Context, []mongodriver.IndexModel, ...*options.CreateIndexesOptions) ([]string, error) {
	v.manyCalls++
	return []string{"idx2", "idx3"}, v.createManyErr
}

func TestEnsureIndexesHelper(t *testing.T) {
	chatView := &fakeIndexView{}
	msgView := &fakeIndexView{}
	db := fakeIndexDB{cols: map[string]*fakeIndexCollection{
		"chatrooms": {view: chatView},
		"messages":  {view: msgView},
	}}

	if err := ensureIndexes(context.Background(), db); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if chatView.oneCalls != 1 || msgView.manyCalls != 1 {
		t.Fatalf("expected both index operations to be called")
	}

	chatView.createOneErr = errors.New("create one failed")
	if err := ensureIndexes(context.Background(), db); err == nil || err.Error() != "create one failed" {
		t.Fatalf("expected create one error, got %v", err)
	}
	chatView.createOneErr = nil

	msgView.createManyErr = errors.New("create many failed")
	if err := ensureIndexes(context.Background(), db); err == nil || err.Error() != "create many failed" {
		t.Fatalf("expected create many error, got %v", err)
	}
}

func TestDuplicateKeyAndDatabaseName(t *testing.T) {
	if isDuplicateKey(nil) {
		t.Fatalf("nil error should not be duplicate")
	}
	if isDuplicateKey(errors.New("x")) {
		t.Fatalf("non write exception should not be duplicate")
	}
	if isDuplicateKey(mongodriver.WriteException{WriteErrors: []mongodriver.WriteError{{Code: 42}}}) {
		t.Fatalf("wrong code should not be duplicate")
	}
	if !isDuplicateKey(mongodriver.WriteException{WriteErrors: []mongodriver.WriteError{{Code: 11000}}}) {
		t.Fatalf("duplicate key code should be recognized")
	}

	if got := DatabaseNameFromURI("::::"); got != "easychat" {
		t.Fatalf("expected fallback db name, got %s", got)
	}
	if got := DatabaseNameFromURI("mongodb://localhost:27017"); got != "easychat" {
		t.Fatalf("expected fallback db name, got %s", got)
	}
	if got := DatabaseNameFromURI("mongodb://localhost:27017/mydb?authSource=admin"); got != "mydb" {
		t.Fatalf("expected db name mydb, got %s", got)
	}
}

func TestExportedConstructorsAndHelpers(t *testing.T) {
	client, err := mongodriver.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		t.Fatalf("failed to create mongo client: %v", err)
	}
	db := client.Database("easychat")

	if NewUserRepository(db) == nil {
		t.Fatalf("expected user repository")
	}
	if NewChatRoomRepository(db) == nil {
		t.Fatalf("expected chatroom repository")
	}
	if NewMessageRepository(db) == nil {
		t.Fatalf("expected message repository")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Connect(ctx, "mongodb://localhost:27017"); err == nil {
		t.Fatalf("expected connect error with canceled context")
	}
	if err := EnsureIndexes(ctx, db); err == nil {
		t.Fatalf("expected ensure indexes error with canceled context")
	}
}
