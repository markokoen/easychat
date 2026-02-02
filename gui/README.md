# EasyChat GUI (React + Next.js)

This is a lightweight UI for local testing of the Go service.

## What it does

- Create a chatroom with one click.
- Use multiple user names in the same room.
- Open one websocket session per user.
- Send messages from any user and receive realtime updates on the same page.

## Requirements

- Go backend running on `http://localhost:8080`
- Node.js 18+

## Run

```bash
cd gui
npm install
npm run dev
```

Open `http://localhost:3000`.

## Environment variables (optional)

- `EASYCHAT_API_BASE_URL` (default: `http://localhost:8080`)
- `NEXT_PUBLIC_EASYCHAT_WS_URL` (default: `ws://localhost:8080`)

If you need custom values:

```bash
cp .env.example .env.local
```
