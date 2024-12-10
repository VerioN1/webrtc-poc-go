package webrtc_media

import (
	"fmt"
	"io"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

var defaultPeerCfg = webrtc.Configuration{
	SDPSemantics: webrtc.SDPSemanticsUnifiedPlan,
	ICEServers: []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

type WebRTCEngine struct {
	cfg         webrtc.Configuration
	mediaEngine webrtc.MediaEngine
	api         *webrtc.API
}

func NewWebRTCEngine() *WebRTCEngine {
	w := &WebRTCEngine{
		mediaEngine: webrtc.MediaEngine{},
		cfg:         defaultPeerCfg,
	}
	if err := w.mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			Channels:     0,
			SDPFmtpLine:  "profile-level-id=42e01f;",
			RTCPFeedback: nil,
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}
	// if err := w.mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
	// 	RTPCodecCapability: webrtc.RTPCodecCapability{
	// 		MimeType:     webrtc.MimeTypeOpus,
	// 		ClockRate:    48000,
	// 		Channels:     2,
	// 		SDPFmtpLine:  "minptime=10;useinbandfec=1",
	// 		RTCPFeedback: nil,
	// 	},
	// 	PayloadType: 111,
	// }, webrtc.RTPCodecTypeAudio); err != nil {
	// 	panic(err)
	// }
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(&w.mediaEngine, i); err != nil {
		panic(err)
	}

	intervalPliFactory, err := intervalpli.NewReceiverInterceptor(intervalpli.GeneratorInterval(time.Second * 2))
	if err != nil {
		panic(err)
	}
	i.Add(intervalPliFactory)
	// Create API with MediaEngine
	w.api = webrtc.NewAPI(webrtc.WithMediaEngine(&w.mediaEngine), webrtc.WithInterceptorRegistry(i))
	return w
}

// Both sides will send and receive. We add transceivers and set OnTrack.
// If local tracks are provided, we add them so this side can send as well.
func (s *WebRTCEngine) CreateSenderReciverClient(offer webrtc.SessionDescription, pc **webrtc.PeerConnection, addVideoTrack **webrtc.TrackLocalStaticSample, stop chan int) (answer webrtc.SessionDescription, err error) {
	*pc, err = s.api.NewPeerConnection(s.cfg)
	fmt.Printf("WebRTCEngine.CreateSenderReceiverClient pc=%p\n", *pc)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Add transceivers for video and audio in sendrecv mode
	videoTransceiver, err := (*pc).AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv},
	)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// audioTransceiver, err := (*pc).AddTransceiverFromKind(
	// 	webrtc.RTPCodecTypeAudio,
	// 	webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv},
	// )
	// if err != nil {
	// 	return webrtc.SessionDescription{}, err
	// }

	// Handle incoming tracks and send them back
	(*pc).OnTrack(func(t *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("OnTrack received track: %s, codec: %s\n", t.ID(), t.Codec().MimeType)
		// Determine if it's video or audio
		if t.Kind() == webrtc.RTPCodecTypeAudio {
			return
		}
		// Create a local video track to send back data
		fmt.Println("Create local video track")
		localVideoTrack, err := webrtc.NewTrackLocalStaticSample(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
		if err != nil {
			fmt.Println("Failed to create local video track:", err)
			return
		}

		// Replace the track on the video sender with our new local video track
		if err := videoTransceiver.Sender().ReplaceTrack(localVideoTrack); err != nil {
			fmt.Println("Failed to replace video track:", err)
			return
		}

		// Now handle incoming track and write samples to localVideoTrack
		s.handleIncomingTrackWithPLI(t, *pc, stop, localVideoTrack, nil)
		// } else if t.Kind() == webrtc.RTPCodecTypeAudio {
		// 	// Create a local audio track to send back data
		// 	localAudioTrack, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
		// 	if err != nil {
		// 		fmt.Println("Failed to create local audio track:", err)
		// 		return
		// 	}

		// 	// Replace the track on the audio sender with our new local audio track
		// 	if err := audioTransceiver.Sender().ReplaceTrack(localAudioTrack); err != nil {
		// 		fmt.Println("Failed to replace audio track:", err)
		// 		return
		// 	}

		// 	// Now handle incoming track and write samples/rtp to localAudioTrack
		// 	s.handleIncomingTrackWithPLI(t, *pc, stop, nil, localAudioTrack)
		// }
	})

	// Set remote description, create and set local answer
	if err = (*pc).SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}

	answer, err = (*pc).CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	if err = (*pc).SetLocalDescription(answer); err != nil {
		return webrtc.SessionDescription{}, err
	}

	fmt.Println("WebRTCEngine.CreateSenderReceiverClient done")
	return answer, err
}

// This version handles PLI channel and can forward incoming tracks to local tracks if provided
func (s *WebRTCEngine) handleIncomingTrackWithPLI(t *webrtc.TrackRemote, pc *webrtc.PeerConnection, stop chan int, videoTrack *webrtc.TrackLocalStaticSample, audioTrack *webrtc.TrackLocalStaticRTP) {
	if t.Codec().MimeType == webrtc.MimeTypeVP8 ||
		t.Codec().MimeType == webrtc.MimeTypeVP9 ||
		t.Codec().MimeType == webrtc.MimeTypeH264 {

		var pkt rtp.Depacketizer
		switch t.Codec().MimeType {
		case webrtc.MimeTypeVP8:
			pkt = &codecs.VP8Packet{}
		case webrtc.MimeTypeVP9:
			pkt = &codecs.VP9Packet{}
		case webrtc.MimeTypeH264:
			pkt = &codecs.H264Packet{}
		}

		builder := samplebuilder.New(35, pkt, t.Codec().ClockRate)
		for {
			select {
			case <-stop:
				return
			default:
				rtpPacket, _, err := t.ReadRTP()
				if err != nil {
					fmt.Println("ReadRTP error:", err.Error())
					return
				}
				builder.Push(rtpPacket)
				for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
					// Write the decoded sample back to our local video track
					if videoTrack != nil {
						if sample.Duration == 0 {
							// Assuming ~30fps, use ~33ms per frame as a fallback
							sample.Duration = time.Millisecond * 40
						}

						fmt.Println("WriteSample", sample.Duration.Milliseconds())
						if err := videoTrack.WriteSample(*sample); err != nil && err != io.ErrClosedPipe {
							fmt.Println("WriteSample error:", err.Error())
						}
					}
				}
			}
		}
		// } else {
		// Audio loopback
		// rtpBuf := make([]byte, 1400)
		// for {
		// 	select {
		// 	case <-stop:
		// 		return
		// 	default:
		// 		i, _, err := t.Read(rtpBuf)
		// 		if err != nil {
		// 			fmt.Println("Audio Read error:", err.Error())
		// 			return
		// 		}
		// 		if audioTrack != nil {
		// 			if _, werr := audioTrack.Write(rtpBuf[:i]); werr != nil && werr != io.ErrClosedPipe {
		// 				fmt.Print("Audio Write error:", werr.Error())
		// 			}
		// 		}
		// 	}
		// }
	}
}
