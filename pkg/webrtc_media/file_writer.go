package webrtc_media

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
)

type Writer interface {
	Write(p []byte) (n int, err error)
	Close() error
}

type containerWriter struct {
	writer   *ivfwriter.IVFWriter
	fileName string
}

// newContainerWriter initializes a container writer based on the codec.
func newContainerWriter(codecMime string) (*containerWriter, error) {
	var filename string

	switch codecMime {
	case webrtc.MimeTypeVP8, webrtc.MimeTypeVP9:
		filename = "output/user_video.ivf"
		w, err := ivfwriter.New(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create ivf writer: %w", err)
		}
		return &containerWriter{writer: w, fileName: filename}, nil
	// case webrtc.MimeTypeH264:
	// 	filename = "output/user_video.h264"
	// 	w, err := h264writer.New(filename)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to create h264 writer: %w", err)
	// 	}
	// 	return &containerWriter{writer: w, fileName: filename}, nil
	default:
	}
	return nil, fmt.Errorf("unsupported codec: %s", codecMime)
}

// writeSample writes a video sample to the container file.
func (cw *containerWriter) writeSample(data *rtp.Packet) error {
	if cw.writer == nil {
		return fmt.Errorf("writer not initialized")
	}
	err := cw.writer.WriteRTP(data)
	return err
}

// close closes the container file.
func (cw *containerWriter) close() error {
	if cw.writer != nil {
		return cw.writer.Close()
	}
	return nil
}

// func DecodeAndSaveFrames(samplesChan <-chan *media.Sample, stop <-chan struct{}) {
// 	// Initialize VP9 decoder using vpx decoder
// 	// Props: VP9, some default resolution and parameters
// 	// Adjust prop.Video if known (width, height), if not known, decoder must handle dynamically.
// 	decoder, err := vpx.NewDecoder(codec.NewRTPVP9Codec(90000), prop.Video{
// 		Width:  1280,
// 		Height: 720,
// 	}, nil)
// 	if err != nil {
// 		log.Fatalf("Failed to create VP9 decoder: %v", err)
// 	}

// 	frameCounter := 0
// 	for {
// 		select {
// 		case <-stop:
// 			return
// 		case sample, ok := <-samplesChan:
// 			if !ok {
// 				return
// 			}

// 			// Decode the frame
// 			imgYUV, err := decoder.Read(sample.Data)
// 			if err != nil {
// 				log.Printf("Failed to decode frame: %v", err)
// 				continue
// 			}
// 			if imgYUV == nil {
// 				// No frame produced yet, might need more data
// 				continue
// 			}

// 			// Convert YUV to RGBA
// 			rgbaImg := yuvToRGBA(imgYUV)

// 			// Save to disk as PNG
// 			filename := fmt.Sprintf("output/frame_%03d.png", frameCounter%6)
// 			err = savePNG(rgbaImg, filename)
// 			if err != nil {
// 				log.Printf("Failed to save image: %v", err)
// 			} else {
// 				log.Printf("Saved frame to %s", filename)
// 			}
// 			frameCounter++
// 		}
// 	}
// }

func savePNG(img image.Image, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// yuvToRGBA converts a YUV image to RGBA.
// If using pion/mediadevices vpx decoder, `imgYUV` might be a `*image.YCbCr`.
func yuvToRGBA(img image.Image) image.Image {
	// If the decoder returns image.YCbCr:
	if yuv, ok := img.(*image.YCbCr); ok {
		bounds := yuv.Bounds()
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := yuv.YCbCrAt(x, y)
				rgba.Set(x, y, c)
			}
		}
		return rgba
	}
	// If it's already RGBA or another format, just return as is
	return img
}
