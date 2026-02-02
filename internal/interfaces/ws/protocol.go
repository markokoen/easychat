package ws

type Envelope struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
	Payload   any    `json:"payload"`
}

type SendMessagePayload struct {
	Content string `json:"content"`
}

type ReadMessagePayload struct {
	MessageID string `json:"messageId"`
}
