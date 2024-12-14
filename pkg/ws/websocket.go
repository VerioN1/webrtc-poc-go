package ws

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type WebSocketMessage struct {
	Type      string                   `json:"type"`
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Payload   map[string]interface{}   `json:"payload,omitempty"`
	Message   string                   `json:"message,omitempty"`
}

// Thread-safe wrapper for a WebSocket connection
type SafeWebSocket struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// NewSafeWebSocket creates a thread-safe WebSocket wrapper
func NewSafeWebSocket(conn *websocket.Conn) *SafeWebSocket {
	return &SafeWebSocket{
		conn: conn,
	}
}

// Send sends a message to the WebSocket in a thread-safe manner
func (s *SafeWebSocket) Send(msg WebSocketMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("cannot send on a closed WebSocket")
	}
	return s.conn.WriteJSON(msg)
}

// OnMessage sets up a loop to read messages from the WebSocket and calls the provided handler
// for each message received. The handler is invoked from a read goroutine.

func (s *SafeWebSocket) OnMessage(ctx context.Context, handler func(msg WebSocketMessage), onClose func()) {
	go func() {
		defer func() {
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
			onClose()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				var msg WebSocketMessage
				if err := s.conn.ReadJSON(&msg); err != nil {
					fmt.Println("WebSocket read error:", err)
					return
				}
				handler(msg)
			}
		}
	}()
}
