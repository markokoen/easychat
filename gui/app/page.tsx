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

type Envelope = {
  type: string;
  requestId?: string;
  payload?: any;
};

type ChatMessage = {
  id: string;
  senderUserName: string;
  content: string;
  createdAt: string;
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
  const seenMessageIdsRef = useRef<Set<string>>(new Set());
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

  const applyParticipantUpdate = (userId: string, updater: (p: Participant) => Participant) => {
    setParticipants((prev) => prev.map((participant) => (participant.userId === userId ? updater(participant) : participant)));
  };

  const handleEnvelope = (participant: Participant, envelope: Envelope) => {
    switch (envelope.type) {
      case "message.created": {
        const incoming = envelope.payload as ChatMessage;
        if (!incoming?.id || seenMessageIdsRef.current.has(incoming.id)) {
          return;
        }
        seenMessageIdsRef.current.add(incoming.id);
        setMessages((prev) => [...prev, incoming]);
        return;
      }
      case "message.sent":
        pushEvent(`${participant.displayName} sent a message.`);
        return;
      case "message.delivered": {
        const receiptName = envelope.payload?.receipt?.userName ?? "recipient";
        pushEvent(`Delivered to ${receiptName}.`);
        return;
      }
      case "message.read": {
        const receiptName = envelope.payload?.receipt?.userName ?? "user";
        pushEvent(`Read by ${receiptName}.`);
        return;
      }
      case "user.joined":
        pushEvent(`${envelope.payload?.userName ?? "User"} joined chat.`);
        return;
      case "user.left":
        pushEvent(`${envelope.payload?.userName ?? "User"} left chat.`);
        return;
      case "error":
        pushEvent(`Socket error for ${participant.displayName}: ${envelope.payload?.message ?? "unknown error"}`);
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
    seenMessageIdsRef.current.clear();
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
    const envelope: Envelope = {
      type: "message.send",
      requestId,
      payload: { content },
    };

    socket.send(JSON.stringify(envelope));
    applyParticipantUpdate(participant.userId, (current) => ({ ...current, draft: "" }));
  };

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

      <section className="contentGrid">
        <article className="chatCard">
          <h2>Conversation</h2>
          <div className="messageList">
            {messages.length === 0 ? <p className="muted">No messages yet.</p> : null}
            {messages.map((message) => (
              <div key={message.id} className="messageBubble">
                <div className="messageMeta">
                  <strong>{message.senderUserName}</strong>
                  <span>{formatTime(message.createdAt)}</span>
                </div>
                <p>{message.content}</p>
              </div>
            ))}
          </div>
        </article>

        <article className="eventCard">
          <h2>Realtime Events</h2>
          <div className="eventList">
            {events.length === 0 ? <p className="muted">Socket events will appear here.</p> : null}
            {events.map((eventText, index) => (
              <p key={`${eventText}-${index}`}>{eventText}</p>
            ))}
          </div>
        </article>
      </section>

      <section className="participantsCard">
        <h2>Connected Users</h2>
        <div className="participantGrid">
          {participants.length === 0 ? <p className="muted">Create a chatroom to open user sessions.</p> : null}
          {participants.map((participant) => (
            <div key={participant.userId} className="participantTile">
              <div className="participantTop">
                <strong>{participant.displayName}</strong>
                <span className={`status ${participant.status}`}>{participant.status}</span>
              </div>
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
          ))}
        </div>
      </section>
    </main>
  );
}
