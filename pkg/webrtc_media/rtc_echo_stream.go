package webrtc_media

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"path/filepath"
	"time"

	"image/jpeg"

	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpvp8"
	// "github.com/pion/rtp/v2"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/xlab/libvpx-go/vpx"
)

var frameCounter int = 0

// Create a simple RTP processor similar to RTCPReceiver
// le echo handler without gRPC
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
		localVideoTrack, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, "t.ID()", "t.StreamID()")
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
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				errSend := (*pc).WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(t.SSRC())}})
				if errSend != nil {
					fmt.Println(errSend)
				}
			}
		}()
		// go func() {
		// 	for {
		// 		rtcpPackets, _, rtcpErr := videoTransceiver.Sender().ReadRTCP()
		// 		if rtcpErr != nil {
		// 			return
		// 		}

		// 		for _, r := range rtcpPackets {
		// 			if _, isPLI := r.(*rtcp.PictureLossIndication); isPLI {
		// 				fmt.Println("Sending PLI")
		// 				if sendErr := (*pc).WriteRTCP([]rtcp.Packet{
		// 					&rtcp.PictureLossIndication{
		// 						MediaSSRC: uint32(t.SSRC()),
		// 					},
		// 				}); sendErr != nil {
		// 					return
		// 				}
		// 			}
		// 		}
		// 	}
		// }()

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

// Update handleSimpleEchoTrack to use the processor
func (s *WebRTCEngine) handleSimpleEchoTrack(t *webrtc.TrackRemote, stop chan int, videoTrack *webrtc.TrackLocalStaticRTP) {
	if s.watermark != nil {
		fmt.Println("Echo mode with watermark enabled for VP8")
	}
	fmt.Printf("Track codec: %s\n", t.Codec().MimeType)

	// Create a VP8 RTP decoder
	rtpDec := rtpvp8.Decoder{}
	err := rtpDec.Init()
	if err != nil {
		panic(err)
	}

	// Create VP8 decoder
	vp8Dec := &vp8Decoder{}
	err = vp8Dec.initialize()
	if err != nil {
		panic(err)
	}
	defer vp8Dec.close()

	go func() {
		// Buffer to read RTP packets
		rtpBuf := make([]byte, 1500)

		for {
			select {
			case <-stop:
				return
			default:
				// Read RTP packet directly
				n, _, err := t.Read(rtpBuf)
				if err != nil {
					fmt.Println("Read error:", err.Error())
					return
				}

				// Forward the RTP packet directly
				if _, err := videoTrack.Write(rtpBuf[:n]); err != nil && err != io.ErrClosedPipe {
					fmt.Println("Write error:", err.Error())
				}

				// For statistics and frame saving, parse the RTP packet
				rtpPacket := &rtp.Packet{}
				if err := rtpPacket.Unmarshal(rtpBuf[:n]); err != nil {
					fmt.Println("RTP unmarshal error:", err)
					continue
				}

				// Extract VP8 access units from RTP packets
				// au, err := depacketizer.Unmarshal(rtpPacket.Payload)
				// if err != nil {
				// 	fmt.Println("RTP depacketize error:", err)
				// 	continue
				// }
				// Process packet for statistics
				// now := time.Now()
				// if err := processor.ProcessPacket(rtpPacket, now, true); err != nil {
				// 	fmt.Println("RTP process error:", err)
				// 	continue
				// }

				// Extract VP8 access units from RTP packets
				au, err := rtpDec.Decode(rtpPacket)
				if err != nil {
					// Skip non-starting packets or partial packets
					if err != rtpvp8.ErrNonStartingPacketAndNoPrevious && err != rtpvp8.ErrMorePacketsNeeded {
						fmt.Println("RTP decode error:", err)
					}
					continue
				}

				// Decode VP8 access unit to image
				img, err := vp8Dec.decode(au)
				if err != nil {
					fmt.Println("VP8 decode error:", err)
					continue
				}

				// Check if we got a valid image
				if img == nil {
					img, err = s.decodeVP8Frame(au)
					if err != nil {
						fmt.Println("VP8 decode error:", err)
						continue
					}
					fmt.Println("No valid 1mage")
					continue
				}

				// Save frame to disk
				outputDir := "output"
				if _, err := os.Stat(outputDir); os.IsNotExist(err) {
					os.MkdirAll(outputDir, 0755)
				}

				outputPath := filepath.Join(outputDir, fmt.Sprintf("frame-%d.jpeg", frameCounter))
				frameCounter++

				f, err := os.Create(outputPath)
				if err != nil {
					fmt.Println("Error creating output file:", err)
				} else {
					if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
						fmt.Println("Error encoding JPEG:", err)
					}
					f.Close()
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
