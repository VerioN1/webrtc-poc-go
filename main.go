package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	totalPacketsProcessed uint64
	totalPacketErrors     uint64
	lastPacketTimestamp   atomic.Value
)

func websocketServer(w http.ResponseWriter, r *http.Request) {
	lastPacketTimestamp.Store(time.Now())

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("WebSocket upgrade error:", err)
		return
	}
	defer ws.Close()

	peerConnection, err := createPeerConnection()
	if err != nil {
		fmt.Println("Failed to create peer connection:", err)
		return
	}
	defer peerConnection.Close()

	var (
		outputTrackMutex sync.RWMutex
		outputTrack      *webrtc.TrackLocalStaticRTP
		rtpSender        *webrtc.RTPSender
	)
	go func() {
		rtcpBuf := make([]byte, 900)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()
	createOrReplaceOutputTrack := func(codec webrtc.RTPCodecCapability) error {
		outputTrackMutex.Lock()
		defer outputTrackMutex.Unlock()

		if outputTrack != nil {
			if err := peerConnection.RemoveTrack(rtpSender); err != nil {
				return fmt.Errorf("failed to remove existing track: %v", err)
			}
		}

		newTrack, err := webrtc.NewTrackLocalStaticRTP(codec, "video", "pion")
		if err != nil {
			return fmt.Errorf("failed to create output track: %v", err)
		}

		rtpSender, err = peerConnection.AddTrack(newTrack)
		if err != nil {
			return fmt.Errorf("failed to add local track: %v", err)
		}

		outputTrack = newTrack
		return nil
	}

	// Initialize with codec
	if err := createOrReplaceOutputTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9}); err != nil {
		fmt.Println("Initial track creation failed:", err)
		return
	}

	// Connection state handlers with improved logging
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State: %s\n", s)
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateClosed ||
			s == webrtc.PeerConnectionStateDisconnected {
			cancel()
		}
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State: %s\n", connectionState)
		if connectionState == webrtc.ICEConnectionStateFailed ||
			connectionState == webrtc.ICEConnectionStateClosed ||
			connectionState == webrtc.ICEConnectionStateDisconnected {
			fmt.Println("ICE connection state is failed/closed/disconnected")
			if err := peerConnection.Close(); err != nil {
				cancel()
				fmt.Println("Failed to close peer connection:", err)
			}
		}
	})

	// Track handling with context and controlled buffer
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
		for {
			// Read RTP packets being sent to Pion
			rtp, _, readErr := track.ReadRTP()
			if readErr != nil {
				fmt.Printf("read Error:", readErr)
			}

			if writeErr := outputTrack.WriteRTP(rtp); writeErr != nil {
				fmt.Printf("Write Error", writeErr)
			}
		}
	})

	// ICE candidate handling remains similar
	peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		message := WebSocketMessage{
			Type:      "candidate",
			Candidate: &candidate,
		}
		if err := ws.WriteJSON(message); err != nil {
			fmt.Println("WebSocket write error:", err)
		}
	})

	var candidateQueue []webrtc.ICECandidateInit
	remoteDescriptionSet := false
	// WebSocket message processing loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var message WebSocketMessage
			if err := ws.ReadJSON(&message); err != nil {
				fmt.Println("WebSocket read error:", err)
				return
			}

			// Existing message processing logic remains the same
			switch message.Type {
			case "offer":
				sdp := webrtc.SessionDescription{
					Type: webrtc.SDPTypeOffer,
					SDP:  message.SDP,
				}
				if err := peerConnection.SetRemoteDescription(sdp); err != nil {
					fmt.Println("SetRemoteDescription error:", err)
					continue
				}
				remoteDescriptionSet = true

				// Create answer
				answer, err := peerConnection.CreateAnswer(nil)
				if err != nil {
					fmt.Println("CreateAnswer error:", err)
					continue
				}

				// Set local description with modified SDP
				if err := peerConnection.SetLocalDescription(answer); err != nil {
					fmt.Println("SetLocalDescription error:", err)
					continue
				}

				// Send the answer back to the client immediately
				response := WebSocketMessage{
					Type: "answer",
					SDP:  peerConnection.LocalDescription().SDP,
				}
				if err := ws.WriteJSON(response); err != nil {
					fmt.Println("WebSocket write error:", err)
					continue
				}

				// Add any queued ICE candidates now that remote description is set.
				for _, c := range candidateQueue {
					if err := peerConnection.AddICECandidate(c); err != nil {
						fmt.Println("AddICECandidate error:", err)
					}
				}
				candidateQueue = nil

			case "candidate":
				if message.Candidate != nil {
					if remoteDescriptionSet {
						if err := peerConnection.AddICECandidate(*message.Candidate); err != nil {
							fmt.Println("AddICECandidate error:", err)
						}
					} else {
						candidateQueue = append(candidateQueue, *message.Candidate)
					}
				}

			case "re-negotiate":
				// Handle re-negotiation if needed.

			case "message":
				// Handle custom messages.

			case "gameConfig":
				// Handle game configuration messages.

			default:
				fmt.Println("Unknown message type:", message.Type)
			}
		}
	}
}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	// Initialize MediaEngine
	m := &webrtc.MediaEngine{}

	// Register Codec
	codecForVideo := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "profile-id=0; ",
		},
		PayloadType: 98, // Use a dynamic payload type
	}
	if err := m.RegisterCodec(codecForVideo, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	// Create API with MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	// Create PeerConnection
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlan,
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		return nil, err

	}

	return peerConnection, nil
}

func main() {
	// Wrap the server in a loop to restart if it fails
	for {
		// Set up the HTTP server
		http.Handle("/", http.FileServer(http.Dir(".")))
		http.HandleFunc("/ws", websocketServer)

		fmt.Println("Server starting on :9912")
		err := http.ListenAndServe(":9912", nil)
		if err != nil {
			fmt.Println("Server encountered an error:", err)
			// Wait before restarting
			time.Sleep(5 * time.Second)
			fmt.Println("Restarting server...")
			// The loop will restart the server
		}
	}
}
