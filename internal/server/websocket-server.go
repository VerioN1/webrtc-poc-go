// Package webrtc contains a WebRTC server.
package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"webrtc_poc_go/internal/protocols/webrtc"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	pwebrtc "github.com/pion/webrtc/v3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WebSocketServer handles WebSocket connections for WebRTC.
type WebSocketServer struct {
	Address    string
	clients    map[string]*wsClient
	mu         sync.Mutex
	httpServer *http.Server
}

type wsClient struct {
	safeWS    *SafeWebSocket
	pc        *pwebrtc.PeerConnection
	uuid      string
	ctx       context.Context
	ctxCancel context.CancelFunc
}

// NewWebSocketServer creates a new WebSocket server.
func NewWebSocketServer(address string) *WebSocketServer {
	return &WebSocketServer{
		Address: address,
		clients: make(map[string]*wsClient),
	}
}

// Start starts the WebSocket server.
func (s *WebSocketServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)

	s.httpServer = &http.Server{
		Addr:    s.Address,
		Handler: mux,
	}

	log.Printf("WebSocket server starting on %s", s.Address)

	go func() {
		err := s.httpServer.ListenAndServe()
		if err != http.ErrServerClosed {
			log.Printf("WebSocket server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the WebSocket server.
func (s *WebSocketServer) Stop() {
	if s.httpServer != nil {
		s.httpServer.Shutdown(context.Background())
	}
}

func (s *WebSocketServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())

	connectionID := uuid.New().String()
	log.Printf("New WebSocket connection from %s, ID: %s", r.RemoteAddr, connectionID)

	safeWS := NewSafeWebSocket(conn)

	client := &wsClient{
		safeWS:    safeWS,
		uuid:      connectionID,
		ctx:       ctx,
		ctxCancel: cancel,
	}

	s.mu.Lock()
	s.clients[connectionID] = client
	s.mu.Unlock()

	defer func() {
		log.Printf("Closing connection: %s", connectionID)
		safeWS.mu.Lock()
		safeWS.closed = true
		conn.Close()
		safeWS.mu.Unlock()

		cancel()

		s.mu.Lock()
		delete(s.clients, connectionID)
		s.mu.Unlock()

		if client.pc != nil {
			client.pc.Close()
		}
	}()

	// Message handling loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg WebSocketMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// Handle message based on type
			switch msg.Type {
			case "offer":
				s.handleOffer(client, msg.SDP)
			case "candidate":
				if msg.Candidate != nil {
					s.handleCandidate(client, *msg.Candidate)
				}
			}
		}
	}
}

func (s *WebSocketServer) handleOffer(client *wsClient, sdp string) {
	// Create configuration
	config := pwebrtc.Configuration{
		ICEServers: []pwebrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := pwebrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		return
	}

	client.pc = pc

	// Add transceivers for video and audio - this allows bidirectional media
	videoTransceiver, err := pc.AddTransceiverFromKind(
		pwebrtc.RTPCodecTypeVideo,
		pwebrtc.RTPTransceiverInit{Direction: pwebrtc.RTPTransceiverDirectionSendrecv},
	)
	if err != nil {
		log.Printf("Failed to add video transceiver: %v", err)
		return
	}

	// Optionally add audio transceiver
	audioTransceiver, err := pc.AddTransceiverFromKind(
		pwebrtc.RTPCodecTypeAudio,
		pwebrtc.RTPTransceiverInit{Direction: pwebrtc.RTPTransceiverDirectionSendrecv},
	)
	if err != nil {
		log.Printf("Failed to add audio transceiver: %v", err)
		return
	}

	// Set up ICE candidate handling
	pc.OnICECandidate(func(c *pwebrtc.ICECandidate) {
		if c == nil {
			return
		}

		candidate := c.ToJSON()
		msg := WebSocketMessage{
			Type:      "candidate",
			Candidate: &candidate,
		}

		if err := client.safeWS.Send(msg); err != nil {
			log.Printf("Failed to send candidate: %v", err)
		}
	})

	// Connection state callback
	pc.OnConnectionStateChange(func(state pwebrtc.PeerConnectionState) {
		log.Printf("Connection state changed for %s: %s", client.uuid, state.String())
	})

	incomingTracks := []*webrtc.IncomingTrack{}
	// Handle incoming tracks
	pc.OnTrack(func(t *pwebrtc.TrackRemote, receiver *pwebrtc.RTPReceiver) {
		log.Printf("OnTrack received track (echo mode): %s, codec: %s", t.ID(), t.Codec().MimeType)

		// Choose the appropriate transceiver based on track type
		incomingTracks = append(incomingTracks, &webrtc.IncomingTrack{
			Track:    t,
			Receiver: receiver,
		})
		var transceiver *pwebrtc.RTPTransceiver
		if t.Kind() == pwebrtc.RTPCodecTypeVideo {
			transceiver = videoTransceiver
			log.Printf("Processing video track")
		} else if t.Kind() == pwebrtc.RTPCodecTypeAudio {
			transceiver = audioTransceiver
			log.Printf("Processing audio track")
		} else {
			log.Printf("Unknown track type: %s", t.Kind().String())
			return
		}

		// Create a local track to send back data
		localTrack, err := pwebrtc.NewTrackLocalStaticRTP(
			t.Codec().RTPCodecCapability,
			t.ID(),
			t.StreamID(),
		)
		if err != nil {
			log.Printf("Failed to create local track: %v", err)
			return
		}

		// Replace the track on the sender with our new local track
		if err := transceiver.Sender().ReplaceTrack(localTrack); err != nil {
			log.Printf("Failed to replace track: %v", err)
			return
		}

		// Echo handler - read RTP packets and write them back
		go func() {
			// For RTCP
			rtcpBuf := make([]byte, 1500)
			go func() {
				for {
					if _, _, rtcpErr := transceiver.Sender().Read(rtcpBuf); rtcpErr != nil {
						return
					}
				}
			}()

			// Create a sample builder
			// builder := pwebrtc.NewSampleBuilder(1000, pkt, t.Codec().ClockRate)

			var packetCount int
			for {
				// Read RTP packet
				rtpPacket, _, err := t.ReadRTP()
				if err != nil {
					log.Printf("ReadRTP error: %v", err)
					return
				}

				packetCount++
				if packetCount%100 == 0 {
					log.Printf("Processed %d packets for %s track", packetCount, t.Kind().String())
				}

				// Push packet to builder
				// builder.Push(rtpPacket)
				if err := localTrack.WriteRTP(rtpPacket); err != nil {
					log.Printf("Failed to write RTP: %v", err)
					return
				}

				// Get complete samples and write them
				// for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
				// 	if err := localTrack.WriteSample(*sample); err != nil {
				// 		log.Printf("WriteSample error: %v", err)
				// 	}
				// }
			}
		}()
	})

	// Set the remote description
	offer := pwebrtc.SessionDescription{
		Type: pwebrtc.SDPTypeOffer,
		SDP:  sdp,
	}

	if err := pc.SetRemoteDescription(offer); err != nil {
		log.Printf("Failed to set remote description: %v", err)
		return
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("Failed to create answer: %v", err)
		return
	}

	// Set the local description
	if err := pc.SetLocalDescription(answer); err != nil {
		log.Printf("Failed to set local description: %v", err)
		return
	}

	// Send answer
	msg := WebSocketMessage{
		Type: "answer",
		SDP:  answer.SDP,
	}

	if err := client.safeWS.Send(msg); err != nil {
		log.Printf("Failed to send answer: %v", err)
	}

	// Create a stream instance that saves to files
	fileStream := &FileOutputMediaStream{
		outputDir: "output",
	}

	// Apply ToStream to process media
	medias, err := webrtc.ToStream(pc, &fileStream)
	if err != nil {
		log.Printf("Failed to map WebRTC to stream: %v", err)
		return
	}

	log.Printf("Successfully mapped %d media tracks for saving", len(medias))
}

func (s *WebSocketServer) handleCandidate(client *wsClient, candidate pwebrtc.ICECandidateInit) {
	if client.pc == nil {
		return
	}

	if err := client.pc.AddICECandidate(candidate); err != nil {
		log.Printf("Failed to add ICE candidate: %v", err)
	}
}

// Usage example (in main.go or equivalent):
//
// func main() {
//     server := NewWebSocketServer("localhost:8080")
//     err := server.Start()
//     if err != nil {
//         log.Fatalf("Failed to start server: %v", err)
//     }
//
//     // Wait for termination signal
//     c := make(chan os.Signal, 1)
//     signal.Notify(c, os.Interrupt)
//     <-c
//
//     server.Stop()
// }
