"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";

type UserSummary = {
  id: string;
  displayName: string;
};

type LoginResponse = {
  token: string;
  user: UserSummary;
};

type ChatRoomResponse = {
  id: string;
  reference: string;
  users: UserSummary[];
};

type DeliveryReceipt = {
  userId: string;
  userName: string;
  status: string;
  sentAt: string;
  deliveredAt?: string;
};

type ReadReceipt = {
  userId: string;
  userName: string;
  readAt: string;
};

type Envelope = {
  type: string;
  requestId?: string;
  payload?: unknown;
};

type ChatMessage = {
  id: string;
  chatRoomId: string;
  senderUserId: string;
  senderUserName: string;
  content: string;
  createdAt: string;
  deliveryReceipts: DeliveryReceipt[];
  readReceipts: ReadReceipt[];
};

type DeliveryEventPayload = {
  messageId: string;
  receipt: DeliveryReceipt;
};

type ReadEventPayload = {
  messageId: string;
  receipt: ReadReceipt;
};

type Participant = {
  userId: string;
  displayName: string;
  token: string;
  status: "connecting" | "connected" | "disconnected" | "error";
  draft: string;
};

const WS_BASE = process.env.NEXT_PUBLIC_EASYCHAT_WS_URL ?? "ws://localhost:8080";

function parseUserNames(raw: string): string[] {
  const unique = new Set<string>();
  raw
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
    .forEach((name) => unique.add(name));
  return Array.from(unique);
}

function formatTime(iso: string): string {
  if (!iso) {
    return "";
  }
  const dt = new Date(iso);
  return Number.isNaN(dt.getTime()) ? "" : dt.toLocaleTimeString();
}

function isDeliveryPayload(payload: unknown): payload is DeliveryEventPayload {
  if (!payload || typeof payload !== "object") {
    return false;
  }
  const candidate = payload as DeliveryEventPayload;
  return typeof candidate.messageId === "string" && !!candidate.receipt && typeof candidate.receipt.userId === "string";
}

function isReadPayload(payload: unknown): payload is ReadEventPayload {
  if (!payload || typeof payload !== "object") {
    return false;
  }
  const candidate = payload as ReadEventPayload;
  return typeof candidate.messageId === "string" && !!candidate.receipt && typeof candidate.receipt.userId === "string";
}

function upsertDeliveryReceipts(receipts: DeliveryReceipt[], receipt: DeliveryReceipt): DeliveryReceipt[] {
  const index = receipts.findIndex((item) => item.userId === receipt.userId);
  if (index === -1) {
    return [...receipts, receipt];
  }
  const next = [...receipts];
  next[index] = receipt;
  return next;
}

function upsertReadReceipts(receipts: ReadReceipt[], receipt: ReadReceipt): ReadReceipt[] {
  const index = receipts.findIndex((item) => item.userId === receipt.userId);
  if (index === -1) {
    return [...receipts, receipt];
  }
  const next = [...receipts];
  next[index] = receipt;
  return next;
}

function mergeMessage(existing: ChatMessage, incoming: ChatMessage): ChatMessage {
  let merged = { ...existing, ...incoming };
  for (const receipt of incoming.deliveryReceipts ?? []) {
    merged = { ...merged, deliveryReceipts: upsertDeliveryReceipts(merged.deliveryReceipts ?? [], receipt) };
  }
  for (const receipt of incoming.readReceipts ?? []) {
    merged = { ...merged, readReceipts: upsertReadReceipts(merged.readReceipts ?? [], receipt) };
  }
  return merged;
}

function formatReceiptNames<T extends { userName: string }>(items: T[]): string {
  return items.map((item) => item.userName).filter(Boolean).join(", ");
}

export default function Page() {
  const [chatRoomReference, setChatRoomReference] = useState("");
  const [userNamesInput, setUserNamesInput] = useState("Alex, Sam, Priya");
  const [chatRoomId, setChatRoomId] = useState("");
  const [participants, setParticipants] = useState<Participant[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [events, setEvents] = useState<string[]>([]);
  const [isBusy, setIsBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");

  const socketsRef = useRef<Map<string, WebSocket>>(new Map());
  const requestCounterRef = useRef(1);

  const userNames = useMemo(() => parseUserNames(userNamesInput), [userNamesInput]);

  const closeAllSockets = () => {
    socketsRef.current.forEach((socket) => {
      try {
        socket.close();
      } catch {
        // ignore close errors
      }
    });
    socketsRef.current.clear();
  };

  const pushEvent = (text: string) => {
    const now = new Date().toLocaleTimeString();
    setEvents((prev) => [`${now} · ${text}`, ...prev].slice(0, 80));
  };

  const applyParticipantUpdate = (userId: string, updater: (participant: Participant) => Participant) => {
    setParticipants((prev) => prev.map((participant) => (participant.userId === userId ? updater(participant) : participant)));
  };

  const addOrUpdateMessage = (incoming: ChatMessage) => {
    setMessages((prev) => {
      const index = prev.findIndex((message) => message.id === incoming.id);
      if (index === -1) {
        return [...prev, incoming].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
      }
      const next = [...prev];
      next[index] = mergeMessage(next[index], incoming);
      return next;
    });
  };

  const updateMessageReceipts = (messageId: string, updater: (message: ChatMessage) => ChatMessage) => {
    setMessages((prev) => prev.map((message) => (message.id === messageId ? updater(message) : message)));
  };

  const handleEnvelope = (participant: Participant, envelope: Envelope) => {
    switch (envelope.type) {
      case "message.created": {
        const incoming = envelope.payload as ChatMessage;
        if (!incoming?.id) {
          return;
        }
        addOrUpdateMessage({
          ...incoming,
          deliveryReceipts: incoming.deliveryReceipts ?? [],
          readReceipts: incoming.readReceipts ?? [],
        });
        return;
      }
      case "message.sent":
        pushEvent(`${participant.displayName} sent a message.`);
        return;
      case "message.delivered": {
        if (!isDeliveryPayload(envelope.payload)) {
          return;
        }
        const { messageId, receipt } = envelope.payload;
        updateMessageReceipts(messageId, (message) => ({
          ...message,
          deliveryReceipts: upsertDeliveryReceipts(message.deliveryReceipts, receipt),
        }));
        pushEvent(`Delivered to ${receipt.userName || "recipient"}.`);
        return;
      }
      case "message.read": {
        if (!isReadPayload(envelope.payload)) {
          return;
        }
        const { messageId, receipt } = envelope.payload;
        updateMessageReceipts(messageId, (message) => ({
          ...message,
          readReceipts: upsertReadReceipts(message.readReceipts, receipt),
        }));
        pushEvent(`Read by ${receipt.userName || "user"}.`);
        return;
      }
      case "user.joined":
        pushEvent(`${(envelope.payload as { userName?: string } | undefined)?.userName ?? "User"} joined chat.`);
        return;
      case "user.left":
        pushEvent(`${(envelope.payload as { userName?: string } | undefined)?.userName ?? "User"} left chat.`);
        return;
      case "error":
        pushEvent(
          `Socket error for ${participant.displayName}: ${
            (envelope.payload as { message?: string } | undefined)?.message ?? "unknown error"
          }`,
        );
        return;
      default:
        return;
    }
  };

  const connectParticipantSocket = (participant: Participant, roomID: string) => {
    const socketURL = `${WS_BASE}/ws/chatrooms/${encodeURIComponent(roomID)}?token=${encodeURIComponent(participant.token)}`;
    const socket = new WebSocket(socketURL);
    socketsRef.current.set(participant.userId, socket);

    socket.onopen = () => {
      applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "connected" }));
      pushEvent(`${participant.displayName} connected.`);
    };

    socket.onclose = () => {
      applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "disconnected" }));
      pushEvent(`${participant.displayName} disconnected.`);
      socketsRef.current.delete(participant.userId);
    };

    socket.onerror = () => {
      applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "error" }));
      pushEvent(`${participant.displayName} connection error.`);
    };

    socket.onmessage = (event) => {
      try {
        const envelope = JSON.parse(event.data) as Envelope;
        handleEnvelope(participant, envelope);
      } catch {
        pushEvent(`Failed to parse socket event for ${participant.displayName}.`);
      }
    };
  };

  const disconnectParticipant = (participant: Participant) => {
    const socket = socketsRef.current.get(participant.userId);
    if (!socket) {
      applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "disconnected" }));
      pushEvent(`${participant.displayName} is already disconnected.`);
      return;
    }

    if (socket.readyState === WebSocket.CLOSING || socket.readyState === WebSocket.CLOSED) {
      socketsRef.current.delete(participant.userId);
      applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "disconnected" }));
      pushEvent(`${participant.displayName} is already disconnecting.`);
      return;
    }

    socket.close(1000, "manual disconnect");
  };

  const connectParticipant = (participant: Participant) => {
    if (!chatRoomId) {
      setErrorMessage("Create a chatroom before reconnecting users.");
      return;
    }

    const existing = socketsRef.current.get(participant.userId);
    if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
      pushEvent(`${participant.displayName} is already connected.`);
      return;
    }

    if (existing && existing.readyState === WebSocket.CLOSING) {
      pushEvent(`${participant.displayName} is disconnecting, try again in a moment.`);
      return;
    }

    socketsRef.current.delete(participant.userId);
    applyParticipantUpdate(participant.userId, (current) => ({ ...current, status: "connecting" }));
    connectParticipantSocket(participant, chatRoomId);
  };

  const toggleParticipantConnection = (participant: Participant) => {
    if (participant.status === "connected" || participant.status === "connecting") {
      disconnectParticipant(participant);
      return;
    }
    connectParticipant(participant);
  };

  const createAndConnectRoom = async (event: FormEvent) => {
    event.preventDefault();

    if (userNames.length < 2) {
      setErrorMessage("Add at least two user names to simulate multi-user chat.");
      return;
    }

    setIsBusy(true);
    setErrorMessage("");
    setChatRoomId("");
    setMessages([]);
    setEvents([]);
    closeAllSockets();

    try {
      const loginResponses: LoginResponse[] = [];

      for (const displayName of userNames) {
        const response = await fetch("/api/easychat/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ displayName }),
        });
        const payload = await response.json();
        if (!response.ok) {
          throw new Error(payload?.message ?? `Login failed for ${displayName}`);
        }
        loginResponses.push(payload as LoginResponse);
      }

      const roomReference = chatRoomReference.trim() || `gui-room-${Date.now()}`;
      const roomCreateResponse = await fetch("/api/easychat/chatrooms", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${loginResponses[0].token}`,
        },
        body: JSON.stringify({
          reference: roomReference,
          users: loginResponses.map((login) => ({ id: login.user.id, displayName: login.user.displayName })),
        }),
      });

      const roomPayload = await roomCreateResponse.json();
      if (!roomCreateResponse.ok) {
        throw new Error(roomPayload?.message ?? "Failed to create chatroom");
      }

      const room = roomPayload as ChatRoomResponse;
      setChatRoomId(room.id);
      setChatRoomReference(room.reference);
      pushEvent(`Chatroom ${room.reference} created.`);

      const nextParticipants: Participant[] = loginResponses.map((login) => ({
        userId: login.user.id,
        displayName: login.user.displayName,
        token: login.token,
        status: "connecting",
        draft: "",
      }));

      setParticipants(nextParticipants);
      nextParticipants.forEach((participant) => connectParticipantSocket(participant, room.id));
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unexpected setup error";
      setErrorMessage(message);
      pushEvent(`Setup failed: ${message}`);
    } finally {
      setIsBusy(false);
    }
  };

  const sendMessage = (participant: Participant) => {
    const content = participant.draft.trim();
    if (!content) {
      return;
    }

    const socket = socketsRef.current.get(participant.userId);
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      setErrorMessage(`Socket is not open for ${participant.displayName}`);
      return;
    }

    const requestId = `${participant.userId}-${requestCounterRef.current++}`;
    socket.send(JSON.stringify({ type: "message.send", requestId, payload: { content } }));
    applyParticipantUpdate(participant.userId, (current) => ({ ...current, draft: "" }));
  };

  const markUnreadAsRead = (participant: Participant) => {
    const socket = socketsRef.current.get(participant.userId);
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return;
    }

    const unreadMessages = messages.filter(
      (message) =>
        message.senderUserId !== participant.userId && !message.readReceipts.some((receipt) => receipt.userId === participant.userId),
    );

    if (unreadMessages.length === 0) {
      return;
    }

    const now = new Date().toISOString();

    for (const message of unreadMessages) {
      const requestId = `${participant.userId}-read-${requestCounterRef.current++}`;
      socket.send(JSON.stringify({ type: "message.read", requestId, payload: { messageId: message.id } }));

      // Optimistic UI so read metadata appears immediately after click.
      updateMessageReceipts(message.id, (current) => ({
        ...current,
        readReceipts: upsertReadReceipts(current.readReceipts, {
          userId: participant.userId,
          userName: participant.displayName,
          readAt: now,
        }),
      }));
    }

    pushEvent(`${participant.displayName} marked ${unreadMessages.length} message(s) as read.`);
  };

  const unreadCount = (participant: Participant) =>
    messages.filter(
      (message) =>
        message.senderUserId !== participant.userId && !message.readReceipts.some((receipt) => receipt.userId === participant.userId),
    ).length;

  useEffect(() => {
    return () => {
      closeAllSockets();
    };
  }, []);

  return (
    <main className="shell">
      <section className="setupCard">
        <div className="setupHeader">
          <h1>EasyChat GUI</h1>
          <p>Create one chatroom, connect multiple named users, and chat in realtime from one page.</p>
        </div>

        <form onSubmit={createAndConnectRoom} className="setupForm">
          <label>
            Chatroom reference
            <input
              value={chatRoomReference}
              onChange={(e) => setChatRoomReference(e.target.value)}
              placeholder="Optional (auto-generated when empty)"
            />
          </label>

          <label>
            User names (comma or newline separated)
            <textarea
              value={userNamesInput}
              onChange={(e) => setUserNamesInput(e.target.value)}
              rows={3}
              placeholder="Alex, Sam, Priya"
            />
          </label>

          <button type="submit" disabled={isBusy}>
            {isBusy ? "Creating room..." : "Create Chatroom + Connect Users"}
          </button>
        </form>

        <div className="quickInfo">
          <span>Users: {userNames.length}</span>
          <span>Room ID: {chatRoomId || "not connected"}</span>
        </div>

        {errorMessage ? <p className="error">{errorMessage}</p> : null}
      </section>

      <section className="participantsCard">
        <h2>Per-User Chat Boxes</h2>
        <p className="helperText">Click inside a user inbox to mark unread messages as read for that user only.</p>

        <div className="participantGrid">
          {participants.length === 0 ? <p className="muted">Create a chatroom to open user sessions.</p> : null}

          {participants.map((participant) => {
            const unread = unreadCount(participant);

            return (
              <div key={participant.userId} className="participantTile">
                <div className="participantTop">
                  <strong>{participant.displayName}</strong>
                  <div className="participantMeta">
                    {unread > 0 ? <span className="badge">{unread} unread</span> : null}
                    <span className={`status ${participant.status}`}>{participant.status}</span>
                  </div>
                </div>
                <div className="sessionActions">
                  <button
                    type="button"
                    className={participant.status === "connected" || participant.status === "connecting" ? "dangerButton" : "secondaryButton"}
                    onClick={() => toggleParticipantConnection(participant)}
                    disabled={!chatRoomId}
                  >
                    {participant.status === "connected" || participant.status === "connecting" ? "Disconnect" : "Connect"}
                  </button>
                </div>

                <div className="inbox" onClick={() => markUnreadAsRead(participant)}>
                  {messages.length === 0 ? <p className="muted">No messages yet.</p> : null}

                  {messages.map((message) => {
                    const mine = message.senderUserId === participant.userId;
                    const recipientCount = Math.max(participants.length - 1, 1);
                    const deliveredRecipients = message.deliveryReceipts.filter(
                      (receipt) => receipt.userId !== message.senderUserId,
                    );
                    const readRecipients = message.readReceipts.filter((receipt) => receipt.userId !== message.senderUserId);
                    const deliveredTotal = deliveredRecipients.length;
                    const readTotal = readRecipients.length;

                    return (
                      <div key={`${participant.userId}-${message.id}`} className={`messageItem ${mine ? "mine" : "theirs"}`}>
                        <div className="messageHead">
                          <strong>{message.senderUserName}</strong>
                          <span>{formatTime(message.createdAt)}</span>
                        </div>
                        <p>{message.content}</p>
                        <div className="receiptMeta">
                          <span>Delivered: {deliveredTotal}/{recipientCount}</span>
                          <span>Read: {readTotal}/{recipientCount}</span>
                        </div>
                        <div className="receiptNames">
                          {deliveredRecipients.length > 0 ? (
                            <small>D · {formatReceiptNames(deliveredRecipients)}</small>
                          ) : null}
                          {readRecipients.length > 0 ? <small>R · {formatReceiptNames(readRecipients)}</small> : null}
                        </div>
                      </div>
                    );
                  })}
                </div>

                <div className="composer">
                  <textarea
                    value={participant.draft}
                    onChange={(e) =>
                      applyParticipantUpdate(participant.userId, (current) => ({ ...current, draft: e.target.value }))
                    }
                    rows={3}
                    placeholder={`Send as ${participant.displayName}`}
                  />
                  <button
                    type="button"
                    onClick={() => sendMessage(participant)}
                    disabled={participant.status !== "connected" || !participant.draft.trim()}
                  >
                    Send Message
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </section>

      <section className="eventCard">
        <h2>Realtime Events</h2>
        <div className="eventList">
          {events.length === 0 ? <p className="muted">Socket events will appear here.</p> : null}
          {events.map((eventText, index) => (
            <p key={`${eventText}-${index}`}>{eventText}</p>
          ))}
        </div>
      </section>
    </main>
  );
}
