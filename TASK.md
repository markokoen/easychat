# EasyChat -- Codex Build Task

This TASK.md defines the full build instructions for **EasyChat**, a
production-grade Golang WebSocket-first chat service.

------------------------------------------------------------------------

## Objective

Implement EasyChat using Clean Architecture (Hexagonal / Ports &
Adapters).

REST is used ONLY for:

-   Authentication
-   Chatroom management

Everything else (messages, receipts, presence) uses WebSockets.

MongoDB provides persistence.

------------------------------------------------------------------------

## Tech Stack

-   Go (modules)
-   MongoDB (official driver)
-   Gorilla WebSocket (or equivalent)
-   JWT authentication
-   OpenAPI 3 + Swagger UI

------------------------------------------------------------------------

## Repository Layout

    easychat/
      cmd/server/main.go

      internal/
        domain/chat
        app/chat
        interfaces/http
        interfaces/ws
        infrastructure/mongo
        infrastructure/auth
        infrastructure/nats
        platform/config
        platform/logger

      docs/
      scripts/
      Makefile
      go.mod
      README.md

------------------------------------------------------------------------

## REST API

Minimal REST endpoints:

    POST /api/v1/auth/login
    POST /api/v1/chatrooms
    GET  /api/v1/chatrooms/{chatRoomId}
    GET  /api/v1/chatrooms/reference/{reference}

Rules:

-   JSON only
-   Errors always `{ "message": "..." }`
-   Bearer auth required except login

------------------------------------------------------------------------

## WebSocket Endpoint

    GET /ws/chatrooms/{chatRoomId}
    Authorization: Bearer <token>

Envelope:

    {
      "type": "string",
      "requestId": "optional",
      "payload": {}
    }

Client → Server:

-   message.send
-   message.read

Server → Client:

-   message.created
-   message.sent
-   message.delivered
-   message.read
-   user.joined
-   user.left

------------------------------------------------------------------------

## Domain Models

User

-   id
-   displayName
-   metadata
-   createdAt

ChatRoom

-   id
-   reference
-   users\[\]
-   createdAt

Message

-   id
-   chatRoomId
-   senderUserId
-   senderUserName
-   content
-   createdAt
-   deliveryReceipts\[\]
-   readReceipts\[\]

DeliveryReceipt

-   userId
-   userName
-   status (SENT \| DELIVERED)
-   sentAt
-   deliveredAt

ReadReceipt

-   userId
-   userName
-   readAt

All timestamps RFC3339.

------------------------------------------------------------------------

## Repository Interfaces

    UserRepository:
      GetByID
      Upsert

    ChatRoomRepository:
      Create
      GetByID
      GetByReference
      AddUser
      RemoveUser

    MessageRepository:
      Create
      GetByID
      ListByChatRoom
      UpsertDeliveryReceipt
      UpsertReadReceipt

------------------------------------------------------------------------

## MongoDB

Collections:

-   users
-   chatrooms
-   messages

Indexes:

    chatrooms.reference
    messages.chatRoomId + createdAt
    messages.deliveryReceipts.userId
    messages.readReceipts.userId

Messages MUST be persisted before WebSocket broadcast.

------------------------------------------------------------------------

## Connection Manager

Central registry:

    map[chatRoomId]map[userId]*Connection

Rules:

-   One writer goroutine per connection
-   Buffered outbound channel
-   Non-blocking broadcast
-   Disconnect slow clients
-   Emit presence on join/leave
-   Cleanup on any error

------------------------------------------------------------------------

## Clean Architecture Dependency Map

    domain <- app <- interfaces
    domain <- infrastructure

    Wiring ONLY in main.go

------------------------------------------------------------------------

## Authentication

JWT bearer tokens.

Flow:

1.  REST login
2.  Receive token
3.  WebSocket upgrade validates token

Auth module fully isolated.

------------------------------------------------------------------------

## Swagger / OpenAPI

-   REST endpoints fully documented
-   WebSocket endpoint included as documentation-only
-   Protocol examples embedded
-   Swagger UI served at `/swagger/index.html`

------------------------------------------------------------------------

## Environment Variables

    MONGO_URI
    NATS_URL
    SERVER_PORT
    AUTH_PROVIDER_TYPE
    JWT_SECRET

------------------------------------------------------------------------

## Error Model

    { "message": "human readable error" }

Used everywhere.

------------------------------------------------------------------------

## Milestones

### A: Bootstrap

-   go.mod
-   config loader
-   logger
-   main.go wiring

### B: Domain + Application

-   entities
-   repository ports
-   use cases

### C: Infrastructure

-   Mongo repositories
-   JWT auth provider

### D: REST Interfaces

-   handlers
-   middleware
-   validation

### E: WebSockets

-   connection manager
-   event routing
-   message persistence
-   receipts
-   presence

### F: Swagger

-   OpenAPI YAML
-   UI hosting

### G: Hardening

-   graceful shutdown
-   ping/pong
-   message limits
-   unit tests

------------------------------------------------------------------------

## Acceptance Criteria

-   JWT login works
-   Chatrooms created + fetched
-   WebSocket requires auth
-   message.send persists + broadcasts message.created
-   message.sent returned to sender
-   message.delivered generated for recipients
-   message.read persisted + broadcast
-   user.joined / user.left emitted
-   No goroutine leaks
-   Errors always `{message}`
-   Clean Architecture respected

------------------------------------------------------------------------

## Non Goals

-   Typing indicators
-   Attachments
-   Editing
-   Federation
-   Push notifications

------------------------------------------------------------------------

## Summary

EasyChat is a deterministic, WebSocket-first Golang chat platform with
MongoDB persistence, Clean Architecture boundaries, replaceable
authentication, explicit lifecycle control, and reserved NATS
extensibility.
