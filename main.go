package main

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"time"
	pkg "webrtc_poc_go/pkg"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

type WebSocketMessage struct {
	Type      string                   `json:"type"`
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Payload   map[string]interface{}   `json:"payload,omitempty"`
	Message   string                   `json:"message,omitempty"`
}

const (
	currentCodec = pkg.CodecVP8
)

var connectionsCounter = 0
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func websocketServer(w http.ResponseWriter, r *http.Request) {
	// lastPacketTimestamp.Store(time.Now())
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	connectionsCounter++
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("WebSocket upgrade error:", err)
		return
	}
	defer ws.Close()
	mirrorChannel := make(chan *image.RGBA)
	peerConnection, mediaStream, err := createPeerConnection(mirrorChannel)
	if err != nil {
		fmt.Println("Failed to create peer connection:", err, mediaStream)
		return
	}
	defer peerConnection.Close()

	addEventListersToPeer(peerConnection, ws, cancel)
	for _, videoTrack := range mediaStream.GetVideoTracks() {
		videoTrack.OnEnded(func(err error) {
			fmt.Println("Track ended", "error", err)
		})

		_, err := peerConnection.AddTransceiverFromTrack(
			videoTrack,
			webrtc.RtpTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendrecv,
			},
		)
		if err != nil {
			panic(err)
		}
		fmt.Println("add video track success")
	}

	vd := pkg.NewVDecoder(currentCodec, mirrorChannel)
	go vd.Save("output/")

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Received track: Kind=%s, Codec=%s\n", track.Codec().ClockRate, track.Codec().MimeType)
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
			go func() {
				ticker := time.NewTicker(time.Second * 13)
				for range ticker.C {
					errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
					if errSend != nil {
						fmt.Println(errSend)
					}
				}
			}()
		}
		for {
			packet, _, readErr := track.ReadRTP()
			if readErr != nil {
				fmt.Println("Read RTP error:", readErr)
				return
			}
			pkg.PushVPPacket(packet)
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
				fmt.Println("Create answer", answer.SDP)
				// Set local description with modified SDP
				if err := peerConnection.SetLocalDescription(answer); err != nil {
					fmt.Println("SetLocalDescription error:", err)
					continue
				}
				fmt.Println("Set remote description")

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

func createPeerConnection(mirrorChannel chan *image.RGBA) (*webrtc.PeerConnection, mediadevices.MediaStream, error) {
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
		PayloadType: 96, // Use a dynamic payload type
	}

	if err := m.RegisterCodec(codecForVideo, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, nil, err
	}

	pkg.InitMediaTracker(mirrorChannel, connectionsCounter)
	vp8Params, err := vpx.NewVP8Params()
	if err != nil {
		panic(err)
	}
	vp8Params.BitRate = 5_000_000
	codecselector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vp8Params),
	)

	codecselector.Populate(m)
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}
	// slog.Info("Created peer connection")

	mediaStream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(constraint *mediadevices.MediaTrackConstraints) {},
		Codec: codecselector,
	})
	if err != nil || mediaStream == nil {
		panic(err)
	}
	fmt.Println("Created mediaStream:", mediaStream)
	return peerConnection, mediaStream, nil
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

func addEventListersToPeer(peerConnection *webrtc.PeerConnection, ws *websocket.Conn, cancel context.CancelFunc) {

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

}
