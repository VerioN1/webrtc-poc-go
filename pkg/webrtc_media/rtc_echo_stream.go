package webrtc_media

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"io"
	"time"

	"image/jpeg"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"github.com/xlab/libvpx-go/vpx"
)

// Simple echo handler without gRPC
func (s *WebRTCEngine) createSimpleEchoSender(offer webrtc.SessionDescription, pc **webrtc.PeerConnection, addVideoTrack **webrtc.TrackLocalStaticSample, stop chan int) (answer webrtc.SessionDescription, err error) {
	*pc, err = s.api.NewPeerConnection(s.cfg)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Add transceivers for video in sendrecv mode
	videoTransceiver, err := (*pc).AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
	)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Handle incoming tracks with simple echo back
	(*pc).OnTrack(func(t *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("OnTrack received track (echo mode): %s, codec: %s\n", t.ID(), t.Codec().MimeType)

		// Handle only video tracks
		if t.Kind() == webrtc.RTPCodecTypeAudio {
			return
		}

		// Create a local video track to send back data
		fmt.Println("Create local video track (echo mode)")
		localVideoTrack, err := webrtc.NewTrackLocalStaticSample(t.Codec().RTPCodecCapability, "t.ID()", "t.StreamID()")
		if err != nil {
			fmt.Println("Failed to create local video track:", err)
			return
		}

		// Replace the track on the video sender with our new local video track
		if err := videoTransceiver.Sender().ReplaceTrack(localVideoTrack); err != nil {
			fmt.Println("Failed to replace video track:", err)
			return
		}

		// Simple echo handler - read RTP packets and write them back
		s.handleSimpleEchoTrack(t, stop, localVideoTrack)
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

	fmt.Println("CreateSimpleEchoSender done")
	return answer, err
}

// Add a new handler for simple echo-back without gRPC
func (s *WebRTCEngine) handleSimpleEchoTrack(t *webrtc.TrackRemote, stop chan int, videoTrack *webrtc.TrackLocalStaticSample) {
	if s.watermark != nil {
		fmt.Println("Echo mode with watermark enabled for VP8")
	}
	fmt.Printf("Track codec: %s\n", t.Codec().MimeType)

	var pkt rtp.Depacketizer
	switch t.Codec().MimeType {
	case webrtc.MimeTypeVP8:
		pkt = &codecs.VP8Packet{}
	case webrtc.MimeTypeVP9:
		pkt = &codecs.VP9Packet{}
	case webrtc.MimeTypeH264:
		pkt = &codecs.H264Packet{}
	}

	builder := samplebuilder.New(350, pkt, t.Codec().ClockRate)

	go func() {
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
					// Process VP8 frames with watermark
					if s.watermark != nil && t.Codec().MimeType == webrtc.MimeTypeVP8 {
						// fmt.Println("Processing VP8 frame with watermark, data size:", len(sample.Data))
						processedData := sample.Data
						// processedData, err := s.processVP8FrameWithWatermark(sample.Data, sample.Duration)
						if err == nil {
							// Use processed data with watermark
							processedSample := media.Sample{
								Data:     processedData,
								Duration: sample.Duration,
							}
							if err := videoTrack.WriteSample(processedSample); err != nil && err != io.ErrClosedPipe {
								fmt.Println("WriteSample error:", err.Error())
							}
							continue
						}
						fmt.Println("Failed to process VP8 frame:", err)
					}

					// Fallback to original sample if processing fails or not VP8
					if err := videoTrack.WriteSample(*sample); err != nil && err != io.ErrClosedPipe {
						fmt.Println("WriteSample error:", err.Error())
					}
				}
			}
		}
	}()
}

// decodeVP8Frame decodes a VP8 frame to RGBA image
func (s *WebRTCEngine) decodeVP8Frame(frameData []byte) (*image.RGBA, error) {
	// Initialize VP8 decoder
	decoderCtx := vpx.NewCodecCtx()
	defer vpx.CodecDestroy(decoderCtx)

	decoderIface := vpx.DecoderIfaceVP8()
	if err := vpx.Error(vpx.CodecDecInitVer(decoderCtx, decoderIface, nil, 0, vpx.DecoderABIVersion)); err != nil {
		return nil, fmt.Errorf("failed to initialize VP8 decoder: %v", err)
	}

	// Decode frame
	if err := vpx.Error(vpx.CodecDecode(decoderCtx, string(frameData), uint32(len(frameData)), nil, 0)); err != nil {
		return nil, fmt.Errorf("failed to decode VP8 frame: %v", err)
	}

	// Get decoded image
	var iter vpx.CodecIter
	img := vpx.CodecGetFrame(decoderCtx, &iter)
	if img == nil {
		return nil, fmt.Errorf("no frame decoded")
	}

	// Convert to RGBA
	img.Deref()
	rgba := img.ImageRGBA()
	if rgba == nil {
		return nil, fmt.Errorf("failed to convert to RGBA")
	}

	return rgba, nil
}

// encodeWithFrameSender uses InitEncoderFrameSender to encode a single RGBA image
func (s *WebRTCEngine) encodeWithFrameSender(rgba *image.RGBA, duration time.Duration) ([]byte, error) {
	// Create channels for communication
	inputChan := make(chan []byte, 1)
	resultChan := make(chan []byte, 1)
	errChan := make(chan error, 1)
	doneChan := make(chan struct{})

	// Create a wrapper that captures the encoded output
	captureWriter := &sampleCaptureWriter{
		resultChan: resultChan,
		errChan:    errChan,
		doneChan:   doneChan,
	}

	// Convert RGBA to JPEG for processing
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, nil); err != nil {
		return nil, fmt.Errorf("failed to encode RGBA to JPEG: %v", err)
	}

	// Start the encoder
	go InitEncoderFrameSender(captureWriter, inputChan, s.watermark)

	// Send the frame and close the channel
	inputChan <- buf.Bytes()
	close(inputChan)

	// Wait for result or error
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("encoding timed out")
	}
}

// SampleCaptureWriter captures the first sample written to it
type sampleCaptureWriter struct {
	resultChan chan []byte
	errChan    chan error
	doneChan   chan struct{}
}

func (w *sampleCaptureWriter) WriteSample(sample media.Sample) error {
	select {
	case <-w.doneChan:
		return io.ErrClosedPipe
	default:
		w.resultChan <- sample.Data
		close(w.doneChan)
		return nil
	}
}

// applyWatermark adds watermark to an RGBA image
func (s *WebRTCEngine) applyWatermark(rgba *image.RGBA) {
	if s.watermark == nil {
		return
	}

	bounds := rgba.Bounds()
	wmBounds := s.watermark.Bounds()

	// Tile the watermark across the frame
	for y := bounds.Min.Y; y < bounds.Max.Y; y += wmBounds.Dy() {
		for x := bounds.Min.X; x < bounds.Max.X; x += wmBounds.Dx() {
			r := image.Rectangle{
				Min: image.Point{x, y},
				Max: image.Point{x + wmBounds.Dx(), y + wmBounds.Dy()},
			}
			// Ensure we don't draw outside frame bounds
			r = r.Intersect(bounds)
			draw.Draw(rgba, r, s.watermark, image.Point{0, 0}, draw.Over)
		}
	}
}

// Replace processVP8FrameWithWatermark to use the new function
func (s *WebRTCEngine) processVP8FrameWithWatermark(frameData []byte, duration time.Duration) ([]byte, error) {
	// Decode the VP8 frame to RGBA
	rgba, err := s.decodeVP8Frame(frameData)
	if err != nil {
		return nil, err
	}

	// Skip explicit watermarking as InitEncoderFrameSender handles it
	return s.encodeWithFrameSender(rgba, duration)
}
