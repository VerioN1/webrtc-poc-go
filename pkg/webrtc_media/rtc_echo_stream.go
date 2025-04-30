package webrtc_media

import (
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Create simple echo handler
func (s *WebRTCEngine) createSimpleEchoSender(offer webrtc.SessionDescription, pc **webrtc.PeerConnection, addVideoTrack **webrtc.TrackLocalStaticSample, stop chan int) (answer webrtc.SessionDescription, err error) {
	*pc, err = s.api.NewPeerConnection(s.cfg)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	videoTransceiver, err := (*pc).AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv},
	)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	(*pc).OnTrack(func(t *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if t.Kind() == webrtc.RTPCodecTypeAudio {
			return
		}

		localVideoTrack, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
		if err != nil {
			return
		}

		if err := videoTransceiver.Sender().ReplaceTrack(localVideoTrack); err != nil {
			return
		}

		s.handleSimpleEchoTrack(t, stop, localVideoTrack)
	})

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

	return answer, err
}

// initVP8Decoder creates and initializes a VP8 decoder
func (s *WebRTCEngine) initVP8Decoder() (*vp8Decoder, error) {
	decoder := &vp8Decoder{}
	if err := decoder.initialize(); err != nil {
		return nil, err
	}
	return decoder, nil
}

// processAndSaveFrame decodes VP8 frame and saves it as JPEG
func (s *WebRTCEngine) processAndSaveFrame(decoder *vp8Decoder, frameData []byte, outputDir string, frameIndex int) error {
	// Decode the frame
	img, err := decoder.decode(frameData)
	if err != nil || img == nil {
		return err
	}

	// Save as JPEG
	outputPath := filepath.Join(outputDir, fmt.Sprintf("frame-%d.jpeg", frameIndex%5))
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}

// handleSimpleEchoTrack handles echo functionality for a remote track
func (s *WebRTCEngine) handleSimpleEchoTrack(t *webrtc.TrackRemote, stop chan int, videoTrack *webrtc.TrackLocalStaticRTP) {
	// Create output directory
	outputDir := "output"
	os.MkdirAll(outputDir, 0755)

	// Create channel for frame processing
	frameCh := make(chan FrameData, 20)

	// Create channel-based IVF writer
	channelWriter, err := NewWithChannels(frameCh, WithCodec(t.Codec().MimeType))
	if err != nil {
		return
	}
	defer channelWriter.Close()

	// Initialize VP8 decoder
	vp8Dec, err := s.initVP8Decoder()
	if err != nil {
		return
	}
	defer vp8Dec.close()

	// Start frame processing goroutine
	go func() {
		frameIndex := 0
		for frame := range frameCh {
			s.processAndSaveFrame(vp8Dec, frame.Data, outputDir, frameIndex)
			frameIndex++
		}
	}()

	// RTP packet processing loop
	rtpBuf := make([]byte, 1500)
	for {
		select {
		case <-stop:
			close(frameCh)
			return
		default:
			// Read RTP packet
			n, _, err := t.Read(rtpBuf)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
					return
				}
				continue
			}

			// Forward the RTP packet directly to the output track
			videoTrack.Write(rtpBuf[:n])

			// Parse the RTP packet
			rtpPacket := &rtp.Packet{}
			if err := rtpPacket.Unmarshal(rtpBuf[:n]); err != nil {
				continue
			}

			// Write to the channel-based IVF writer
			channelWriter.WriteRTP(rtpPacket)
		}
	}
}
