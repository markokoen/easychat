import { NextRequest, NextResponse } from "next/server";

const API_BASE = process.env.EASYCHAT_API_BASE_URL ?? "http://localhost:8080";

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const authorization = request.headers.get("authorization") ?? "";

    const upstream = await fetch(`${API_BASE}/api/v1/chatrooms`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: authorization,
      },
      body: JSON.stringify(body),
      cache: "no-store",
    });

    const payload = await upstream.json();
    return NextResponse.json(payload, { status: upstream.status });
  } catch {
    return NextResponse.json({ message: "GUI proxy chatroom creation failed" }, { status: 500 });
  }
}
