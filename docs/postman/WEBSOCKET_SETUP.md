# EasyChat WebSocket Setup in Postman

Use this when the imported JSON opens as HTTP tabs.

## 1) Prepare values from REST collection

Run these requests first from `docs/postman/easychat.postman_collection.json`:

1. `Auth / Login`
2. `ChatRooms / Create ChatRoom`

This gives you:

- `token`
- `chatRoomId`

## 2) Create a real WebSocket request in Postman

1. Open Postman.
2. Click **New**.
3. Select **WebSocket Request**.
4. URL:

```text
ws://localhost:8080/ws/chatrooms/{{chatRoomId}}
```

5. Open **Headers** and add:

- Key: `Authorization`
- Value: `Bearer {{token}}`

6. Click **Connect**.

## 3) Send events

### Send message

```json
{
  "type": "message.send",
  "requestId": "req-1",
  "payload": {
    "content": "Hello from Postman"
  }
}
```

Expected server events include:

- `message.created`
- `message.sent`
- `message.delivered`

### Mark message as read

Replace `<message-id>` with an ID from `message.created`.

```json
{
  "type": "message.read",
  "requestId": "req-2",
  "payload": {
    "messageId": "<message-id>"
  }
}
```

Expected server event:

- `message.read`

## 4) Save inside Postman

1. Click **Save** on the WebSocket tab.
2. Save into a folder like `EasyChat WS`.
3. Reuse the same collection variables (`token`, `chatRoomId`).

## Notes

- Imported collection JSON is HTTP-first in Postman; create WebSocket requests from UI.
- If connection fails, verify:
  - server is running on `:8080`
  - JWT token is valid and not expired
  - chatRoomId exists
