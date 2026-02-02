import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "EasyChat GUI",
  description: "Lightweight UI to create chatrooms and simulate multi-user realtime chat.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
