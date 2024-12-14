package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"webrtc_poc_go/pkg/webrtc_media"
	wsPkg "webrtc_poc_go/pkg/ws"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	peersManagers = webrtc_media.NewPeersManager()
)

func websocketServer(w http.ResponseWriter, r *http.Request) {
	connectionID := uuid.NewString()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("WebSocket upgrade error:", err)
		return
	}

	safeWS := wsPkg.NewSafeWebSocket(ws)
	fmt.Println("New WebSocket connection:", connectionID)

	safeWS.OnMessage(ctx, func(message wsPkg.WebSocketMessage) {
		switch message.Type {
		case "offer":
			p := webrtc_media.NewWebRTCPeer(connectionID)
			peersManagers.AddPeer(connectionID, p)

			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  message.SDP,
			}

			// Since we have no publish/subscribe logic here, we assume sender scenario
			answer, err := p.AnswerSender(offer)
			if err != nil {
				return
			}
			resp := wsPkg.WebSocketMessage{
				Type: "answer",
				SDP:  answer.SDP,
			}
			safeWS.Send(resp)

			p.PC.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
				fmt.Printf("Peer Connection State: %s\n", s)
				if s == webrtc.PeerConnectionStateFailed ||
					s == webrtc.PeerConnectionStateClosed ||
					s == webrtc.PeerConnectionStateDisconnected {
					// cancel()
					fmt.Println("Peer connection state is failed/closed/disconnected")
				}
			})

			p.PC.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
				fmt.Printf("ICE Connection State: %s\n", connectionState)
				if connectionState == webrtc.ICEConnectionStateFailed ||
					connectionState == webrtc.ICEConnectionStateClosed ||
					connectionState == webrtc.ICEConnectionStateDisconnected {
					fmt.Println("ICE connection state is failed/closed/disconnected")
					if err := p.PC.Close(); err != nil {
						// cancel()
						fmt.Println("Failed to close peer connection:", err)
					}
				}
			})
			// // ICE candidate handling remains similar
			fmt.Println("Answering with SDP:", answer.SDP)
			p.PC.OnICECandidate(func(c *webrtc.ICECandidate) {
				if c == nil {
					return
				}
				candidate := c.ToJSON()
				message := wsPkg.WebSocketMessage{
					Type:      "candidate",
					Candidate: &candidate,
				}
				if err := ws.WriteJSON(message); err != nil {
					fmt.Println("WebSocket write error:", err)
				}
			})
		case "candidate":
			p := peersManagers.GetPeer(connectionID)
			if p != nil && message.Candidate != nil {
				err := p.PC.AddICECandidate(*message.Candidate)
				if err != nil {
					fmt.Printf("AddICECandidate error: %v", err)
				}
			}

		default:
			fmt.Println("Unknown message type:", message.Type)
		}
	}, func() {
		peersManagers.RemovePeer(connectionID)
		cancel()
	})

	<-ctx.Done()

}

func createPeerConnection() (*webrtc.PeerConnection, error) {
	// Initialize MediaEngine
	m := &webrtc.MediaEngine{}

	// Register Codec
	codecForVideo := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "profile-id=0; ",
		},
		PayloadType: 98, // Use a dynamic payload type
	}
	if err := m.RegisterCodec(codecForVideo, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}
	// Create API with MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

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
