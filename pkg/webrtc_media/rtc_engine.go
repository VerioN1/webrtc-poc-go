package webrtc_media

import (
	"fmt"
	"image"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"

	"github.com/pion/webrtc/v4/pkg/media"
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

type SampleWriter interface {
	WriteSample(media.Sample) error
}

// Define a struct to pass data between goroutines
type encodedFrame struct {
	frameData []byte
	release   func()
	readStart time.Time
	err       error
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
	switch CurrentVersion {
	case Version8:
		if err := w.mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeVP8,
				ClockRate:   90000,
				Channels:    0,
				SDPFmtpLine: "",
				// SDPFmtpLine:  "profile-level-id=42e01f;packetization-mode=1",
				RTCPFeedback: nil,
			},
			PayloadType: 96,
			// PayloadType: 102,
		}, webrtc.RTPCodecTypeVideo); err != nil {
			panic(err)
		}
	case Version9:
		if err := w.mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeVP9,
				ClockRate:   90000,
				Channels:    0,
				SDPFmtpLine: "",
				// SDPFmtpLine:  "profile-id=0; ",
				RTCPFeedback: nil,
			},
			PayloadType: 98,
		}, webrtc.RTPCodecTypeVideo); err != nil {
			panic(err)
		}
	}

	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(&w.mediaEngine, i); err != nil {
		panic(err)
	}

	intervalPliFactory, err := intervalpli.NewReceiverInterceptor(
		intervalpli.GeneratorInterval(time.Second * 2),
	)
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

		go func() {
			rtcpBuf := make([]byte, 1500)
			for {
				if _, _, rtcpErr := videoTransceiver.Sender().Read(rtcpBuf); rtcpErr != nil {
					return
				}
			}
		}()

		// Now handle incoming track and write samples to localVideoTrack
		s.handleIncomingTrack(t, stop, localVideoTrack, nil)
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
func (s *WebRTCEngine) handleIncomingTrack(t *webrtc.TrackRemote, stop chan int, videoTrack *webrtc.TrackLocalStaticSample, audioTrack *webrtc.TrackLocalStaticRTP) {
	if t.Codec().MimeType == webrtc.MimeTypeVP8 ||
		t.Codec().MimeType == webrtc.MimeTypeVP9 ||
		t.Codec().MimeType == webrtc.MimeTypeH264 {
		sampleChan := make(chan *media.Sample, 1000)
		resultChan := make(chan *image.RGBA, 1000)

		var pkt rtp.Depacketizer
		switch t.Codec().MimeType {
		case webrtc.MimeTypeVP8:
			pkt = &codecs.VP8Packet{}
		case webrtc.MimeTypeVP9:
			pkt = &codecs.VP9Packet{}
		case webrtc.MimeTypeH264:
			pkt = &codecs.H264Packet{}
		}

		go DecodeRawFrame(sampleChan, resultChan)
		go InitNewEncoder(resultChan, videoTrack)

		builder := samplebuilder.New(35, pkt, t.Codec().ClockRate)
		for {
			select {
			case <-stop:
				fmt.Println("Pushed sample to sampleChan stop")
				close(resultChan)
				close(sampleChan)
				return
			default:
				rtpPacket, _, err := t.ReadRTP()
				if err != nil {
					fmt.Println("ReadRTP error:", err.Error())
					return
				}
				builder.Push(rtpPacket)
				for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
					// if err := videoTrack.WriteSample(*sample); err != nil {
					// 	fmt.Println("WriteSample error:", err.Error())
					// 	return
					// }
					sampleChan <- sample
				}
			}
		}
	}
}
