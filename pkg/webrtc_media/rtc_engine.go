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
}

func NewWebRTCEngine() *WebRTCEngine {
	w := &WebRTCEngine{
		mediaEngine: webrtc.MediaEngine{},
		cfg:         defaultPeerCfg,
	}
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

	return w
}

// Both sides will send and receive. We add transceivers and set OnTrack.
// If local tracks are provided, we add them so this side can send as well.
func (s *WebRTCEngine) CreateSenderReciverClient(offer webrtc.SessionDescription, pc **webrtc.PeerConnection, addVideoTrack **webrtc.TrackLocalStaticSample, stop chan int) (answer webrtc.SessionDescription, err error) {

	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(&s.mediaEngine, i); err != nil {
		panic(err)
	}

	// Register a intervalpli factory
	// This interceptor sends a PLI every 3 seconds. A PLI causes a video keyframe to be generated by the sender.
	// This makes our video seekable and more error resilent, but at a cost of lower picture quality and higher bitrates
	// A real world application should process incoming RTCP packets from viewers and forward them to senders
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		panic(err)
	}
	i.Add(intervalPliFactory)

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&s.mediaEngine), webrtc.WithInterceptorRegistry(i))

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	*pc, err = api.NewPeerConnection(config)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	fmt.Printf("WebRTCEngine.CreateSenderReceiverClient pc=%p\n", *pc)

	outputTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		panic(err)
	}

	// Add this newly created track to the PeerConnection
	rtpSender, err := (*pc).AddTrack(outputTrack)
	if err != nil {
		panic(err)
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Set a handler for when a new remote track starts, this handler copies inbound RTP packets,
	// replaces the SSRC and sends them back

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

	(*pc).OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive

		s.handleIncomingTrack(track, stop, outputTrack, nil)
	})

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
		// not calling those functions for now - for the example sake
		// go DecodeRawFrame(sampleChan, resultChan)
		// go InitNewEncoder(resultChan, videoTrack)

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
					if err := videoTrack.WriteSample(*sample); err != nil {
						fmt.Println("WriteSample error:", err.Error())
						return
					}
					// sampleChan <- sample
				}
			}
		}
	}
}

// func (s *webmSaver) InitWriter(isH264 bool, width, height int) {
// 	w, err := os.OpenFile("test.webm", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
// 	if err != nil {
// 		panic(err)
// 	}

// 	videoMimeType := "V_VP8"
// 	if isH264 {
// 		videoMimeType = "V_MPEG4/ISO/AVC"
// 	}

// 	ws, err := webm.NewSimpleBlockWriter(w,
// 		[]webm.TrackEntry{
// 			 {
// 				Name:            "Video",
// 				TrackNumber:     2,
// 				TrackUID:        67890,
// 				CodecID:         videoMimeType,
// 				TrackType:       1,
// 				DefaultDuration: 33333333,
// 				Video: &webm.Video{
// 					PixelWidth:  uint64(width),
// 					PixelHeight: uint64(height),
// 				},
// 			},
// 		})
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Printf("WebM saver has started with video width=%d, height=%d\n", width, height)
// 	s.audioWriter = ws[0]
// 	s.videoWriter = ws[1]
// }
