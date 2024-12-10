package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	// pkg "webrtc_poc_go/pkg"
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

	})

	go func() {
		time.Sleep(5 * time.Minute)
		cancel()
	}()

	<-ctx.Done()

	// peerConnection, err := createPeerConnection()
	// if err != nil {
	// 	fmt.Println("Failed to create peer connection:", err)
	// 	return
	// }
	// defer peerConnection.Close()

	// var (
	// 	outputTrackMutex sync.RWMutex
	// 	outputTrack      *webrtc.TrackLocalStaticSample
	// 	rtpSender        *webrtc.RTPSender
	// )

	// createOrReplaceOutputTrack := func(codec webrtc.RTPCodecCapability) error {
	// 	outputTrackMutex.Lock()
	// 	defer outputTrackMutex.Unlock()

	// 	if outputTrack != nil {
	// 		if err := peerConnection.RemoveTrack(rtpSender); err != nil {
	// 			return fmt.Errorf("failed to remove existing track: %v", err)
	// 		}
	// 	}

	// 	newTrack, err := webrtc.NewTrackLocalStaticSample(codec, "video", "pion")
	// 	if err != nil {
	// 		return fmt.Errorf("failed to create output track: %v", err)
	// 	}

	// 	rtpSender, err = peerConnection.AddTrack(newTrack)
	// 	if err != nil {
	// 		return fmt.Errorf("failed to add local track: %v", err)
	// 	}

	// 	outputTrack = newTrack
	// 	return nil
	// }

	// // Initialize with codec
	// if err := createOrReplaceOutputTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}); err != nil {
	// 	fmt.Println("Initial track creation failed:", err)
	// 	return
	// }

	// // Connection state handlers with improved logging
	// peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
	// 	fmt.Printf("Peer Connection State: %s\n", s)
	// 	if s == webrtc.PeerConnectionStateFailed ||
	// 		s == webrtc.PeerConnectionStateClosed ||
	// 		s == webrtc.PeerConnectionStateDisconnected {
	// 		cancel()
	// 	}
	// })

	// peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
	// 	fmt.Printf("ICE Connection State: %s\n", connectionState)
	// 	if connectionState == webrtc.ICEConnectionStateFailed ||
	// 		connectionState == webrtc.ICEConnectionStateClosed ||
	// 		connectionState == webrtc.ICEConnectionStateDisconnected {
	// 		fmt.Println("ICE connection state is failed/closed/disconnected")
	// 		if err := peerConnection.Close(); err != nil {
	// 			cancel()
	// 			fmt.Println("Failed to close peer connection:", err)
	// 		}
	// 	}
	// })

	// // Track handling with context and controlled buffer
	// peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
	// 	fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
	// 	if track.Kind() == webrtc.RTPCodecTypeVideo {
	// 		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
	// 		go func() {
	// 			ticker := time.NewTicker(time.Second * 3)
	// 			for range ticker.C {
	// 				errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
	// 				if errSend != nil {
	// 					fmt.Println(errSend)
	// 					return
	// 				}
	// 			}
	// 		}()
	// 	}
	// 	for {
	// 		// Read RTP packets being sent to Pion
	// 		rtp, _, readErr := track.ReadRTP()
	// 		if readErr != nil {
	// 			fmt.Printf("read Error:", readErr)
	// 			break
	// 		}

	// 		pkg.H264VideoBuilder.Push(rtp)
	// 		for s := pkg.H264VideoBuilder.Pop(); s != nil; s = pkg.H264VideoBuilder.Pop() {
	// 			if err := (*outputTrack).WriteSample(*s); err != nil && err != io.ErrClosedPipe {
	// 				fmt.Println("WriteSample error:", err)
	// 				break
	// 			}
	// 		}
	// 	}
	// })

	// // ICE candidate handling remains similar
	// peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
	// 	if c == nil {
	// 		return
	// 	}
	// 	candidate := c.ToJSON()
	// 	message := WebSocketMessage{
	// 		Type:      "candidate",
	// 		Candidate: &candidate,
	// 	}
	// 	if err := ws.WriteJSON(message); err != nil {
	// 		fmt.Println("WebSocket write error:", err)
	// 	}
	// })

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
