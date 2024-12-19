package webrtc_media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"time"

	grpc_service "webrtc_poc_go/pkg/grpc_server"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"

	// "github.com/pion/mediadevices/pkg/codec/x264"
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
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(&w.mediaEngine, i); err != nil {
		panic(err)
	}
	// if err := ConfigureNack(&w.mediaEngine, i); err != nil {
	// 	return err
	// }

	intervalPliFactory, err := intervalpli.NewReceiverInterceptor(
	// intervalpli.GeneratorInterval(time.Second * 6),
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
func (s *WebRTCEngine) CreateSenderReciverClient(offer webrtc.SessionDescription, pc **webrtc.PeerConnection, addVideoTrack **webrtc.TrackLocalStaticSample, stop chan int, grpcConnection *grpc_service.GrpcServerManager) (answer webrtc.SessionDescription, err error) {
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

		// Now handle incoming track and write samples to localVideoTrack
		s.handleIncomingTrackWithPLI(t, stop, localVideoTrack, nil, grpcConnection)
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
func (s *WebRTCEngine) handleIncomingTrackWithPLI(t *webrtc.TrackRemote, stop chan int, videoTrack *webrtc.TrackLocalStaticSample, audioTrack *webrtc.TrackLocalStaticRTP, grpcConnection *grpc_service.GrpcServerManager) {
	if t.Codec().MimeType == webrtc.MimeTypeVP8 ||
		t.Codec().MimeType == webrtc.MimeTypeVP9 ||
		t.Codec().MimeType == webrtc.MimeTypeH264 {
		sampleChan := make(chan *media.Sample)
		var pkt rtp.Depacketizer
		switch t.Codec().MimeType {
		case webrtc.MimeTypeVP8:
			pkt = &codecs.VP8Packet{}
		case webrtc.MimeTypeVP9:
			pkt = &codecs.VP9Packet{}
		case webrtc.MimeTypeH264:
			pkt = &codecs.H264Packet{}
		}
		go DecodeVP9AndWriteYUV(sampleChan, grpcConnection)
		InitEncoderFrameSender(videoTrack, grpcConnection.ReceiverChan)
		builder := samplebuilder.New(3500, pkt, t.Codec().ClockRate)
		for {
			select {
			case <-stop:
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
					sampleChan <- sample
					// Write the decoded sample back to our local video track
					// if videoTrack != nil {
					// 	if err := videoTrack.WriteSample(*sample); err != nil && err != io.ErrClosedPipe {
					// 		fmt.Println("WriteSample error:", err.Error())
					// 	}
					// }
				}
			}
		}

	}
}

func isH264KeyFrame(sampleData []byte) bool {
	// Define NAL unit start codes
	startCode3 := []byte{0x00, 0x00, 0x01}
	startCode4 := []byte{0x00, 0x00, 0x00, 0x01}

	// Search for NAL units in the sample data
	offset := 0
	for offset < len(sampleData) {
		// Find the next start code
		start := bytes.Index(sampleData[offset:], startCode3)
		startCodeLength := 3
		if start == -1 {
			start = bytes.Index(sampleData[offset:], startCode4)
			startCodeLength = 4
		}
		if start == -1 {
			break
		}
		start += offset

		// Determine the start of the NAL unit
		nalStart := start + startCodeLength
		if nalStart >= len(sampleData) {
			break
		}

		// Read the NAL unit header byte
		nalHeader := sampleData[nalStart]
		nalUnitType := nalHeader & 0x1F

		// Check if it's an IDR frame (NAL unit type 5)
		if nalUnitType == 5 {
			return true
		}

		// Move to the next NAL unit
		offset = nalStart + 1
	}

	return false
}

// func initEncoderFrameSender(videoTrack *webrtc.TrackLocalStaticSample, receiverChan chan []byte) {
// 	readerAdapter := NewGrpcVideoReaderAdapter(receiverChan, 480, 640)
// 	if err := readerAdapter.Open(); err != nil {
// 		panic(err)
// 	}

// 	medias := readerAdapter.Properties()
// 	if len(medias) == 0 {
// 		panic(" oh no ")
// 	}
// 	prop := medias[0]

// 	vReader, err := readerAdapter.VideoRecord(prop)
// 	if err != nil {
// 		panic(err)
// 	}

// 	params, err := x264.NewParams()
// 	if err != nil {
// 		panic(err)
// 	}

// 	params.BitRate = 1_000_000 // 1 Mbps
// 	params.Preset = x264.PresetVeryfast

// 	encoder, err := params.BuildVideoEncoder(vReader, prop)
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Println("Start reading track", readerAdapter)
// 	var lastFrameTime time.Time
// 	firstFrame := true
// 	go func() {
// 		defer encoder.Close()
// 		for {
// 			frameData, release, err := encoder.Read()
// 			if err != nil {
// 				if err != io.EOF {
// 					fmt.Println("encoder read error:", err)
// 				}
// 				break
// 			}

// 			currentTime := time.Now()

// 			// Calculate frame duration
// 			var frameDuration time.Duration
// 			if firstFrame {
// 				// For the first frame, we have no previous timestamp, so let's set a nominal duration
// 				frameDuration = time.Millisecond * 33
// 				firstFrame = false
// 			} else {
// 				frameDuration = currentTime.Sub(lastFrameTime)
// 			}

// 			// Update lastFrameTime for next iteration
// 			lastFrameTime = currentTime

// 			// Create the sample with the dynamically computed duration
// 			sample := media.Sample{
// 				Data:     frameData,
// 				Duration: frameDuration,
// 			}
// 			fmt.Println("Received frame, sending to encoder", frameDuration)
// 			if err := videoTrack.WriteSample(sample); err != nil && err != io.ErrClosedPipe {
// 				fmt.Println("WriteSample error:", err.Error())
// 			}

// 			if release != nil {
// 				release()
// 			}
// 		}
// 	}()
// }

func InitEncoderFrameSender(videoTrackSampleWriter SampleWriter, receiverChan chan []byte) {
	// readerAdapter := NewGrpcVideoReaderAdapter(receiverChan, 480, 640)
	// ctx := context.Background()
	// encoder, err := NewEncoder(Version9, 480, 640, 1)
	// if err != nil {
	// 	log.Println("Failed to create encoder:", err)
	// 	return
	// }
	log.Println("Start reading track")
	firstFrame := true
	var lastFrameTime time.Time
	go func() {
		for frameData := range receiverChan {
			// startRead := time.Now()
			// img, err := jpeg.Decode(bytes.NewReader(frameData))
			// if err != nil {
			// 	// If decoding fails, log the error and return EOF or handle it as you wish
			// 	log.Printf("Failed to decode JPEG: %v", err)
			// 	continue
			// }
			// yuvFrame, err := imageToYUV(img)
			// if err != nil {
			// 	log.Println("Failed to convert image to YUV:", err)
			// 	continue
			// }
			// // log.Println(img.At(2, 4))
			// log.Println("encoder.Read took %v", time.Since(startRead))
			// encodedFrame, err := encoder.Encode(ctx, yuvFrame)
			// if err != nil {
			// 	log.Println("Failed to encode frame:", err)
			// 	continue
			// }
			currentTime := time.Now()
			var frameDuration time.Duration
			if firstFrame {
				frameDuration = 33 * time.Millisecond
				firstFrame = false
			} else {
				frameDuration = currentTime.Sub(lastFrameTime)
			}
			lastFrameTime = currentTime
			fmt.Println("Received frame, sending to WriteSampler", frameDuration, len(frameData))
			sample := media.Sample{
				Data:     frameData,
				Duration: frameDuration,
			}

			if err := videoTrackSampleWriter.WriteSample(sample); err != nil && err != io.ErrClosedPipe {
				log.Println("WriteSample error:", err)
			}

		}
	}()
}

func imageToYUV(img image.Image) (*image.YCbCr, error) {
	bounds := img.Bounds()
	yuvImg := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// Convert RGBA values to 8-bit
			rr := uint8(r >> 8)
			gg := uint8(g >> 8)
			bb := uint8(b >> 8)
			// Convert RGB to YCbCr
			yy, cb, cr := color.RGBToYCbCr(rr, gg, bb)
			// Assign Y value
			yuvImg.Y[yuvImg.YOffset(x, y)] = yy
			// Assign Cb and Cr values
			if x%2 == 0 && y%2 == 0 {
				offset := yuvImg.COffset(x, y)
				yuvImg.Cb[offset] = cb
				yuvImg.Cr[offset] = cr
			}
		}
	}
	return yuvImg, nil
}
