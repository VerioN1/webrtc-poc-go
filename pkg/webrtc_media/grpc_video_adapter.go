package webrtc_media

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"log"

	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
)

var (
	imgChan chan image.Image
)

// GrpcVideoReaderAdapter reads frames from a gRPC response channel and implements the video.Reader interface.
type GrpcVideoReaderAdapter struct {
	doneCh    chan struct{}
	grpcChan  <-chan []byte // channel receiving raw RGBA frames from the server
	width     int
	height    int
	frameSize int
}

// NewGrpcVideoReaderAdapter creates a new adapter.
// width and height specify the frame dimensions.
// grpcChan is the channel you receive frames from.
func NewGrpcVideoReaderAdapter(grpcChan <-chan []byte, width, height int) *GrpcVideoReaderAdapter {
	return &GrpcVideoReaderAdapter{
		grpcChan: grpcChan,
		width:    width,
		height:   height,
	}
}

func (a *GrpcVideoReaderAdapter) Open() error {
	a.doneCh = make(chan struct{})
	return nil
}

func (a *GrpcVideoReaderAdapter) Close() error {
	if a.doneCh != nil {
		close(a.doneCh)
	}
	return nil
}

// VideoRecord returns a video.Reader that fetches frames from grpcChan
// and returns them as RGBA images.
func (a *GrpcVideoReaderAdapter) VideoRecord(p prop.Media) (video.Reader, error) {
	closed := a.doneCh
	log.Println("Received frame data with size:")
	// imageCount := 0

	r := video.ReaderFunc(func() (image.Image, func(), error) {
		select {
		case <-closed:
			return nil, func() {}, io.EOF
		case frameData, ok := <-a.grpcChan:
			if !ok {
				// The channel is closed by server or end of stream
				return nil, func() {}, io.EOF
			}

			img, err := jpeg.Decode(bytes.NewReader(frameData))
			if err != nil {
				// If decoding fails, log the error and return EOF or handle it as you wish
				log.Printf("Failed to decode JPEG: %v", err)
				return nil, func() {}, io.EOF
			}

			// filename := fmt.Sprintf("output/received_image_%d.jpg", imageCount%10)
			// imageCount++
			// // Create the output file
			// outFile, err := os.Create(filename)
			// if err != nil {
			// 	fmt.Printf("Failed to create file: %v\n", err)
			// }

			// // Encode the image to the file as JPEG
			// err = jpeg.Encode(outFile, img, nil)
			// if err != nil {
			// 	fmt.Printf("Failed to encode image: %v\n", err)
			// }

			// fmt.Println("Image successfully saved to", filename)
			// outFile.Close()

			// log.Println("Received JPEG frame, decoded image with size:", img.Bounds())
			return img, func() {}, nil
		}
	})

	return r, nil
}

func (a *GrpcVideoReaderAdapter) Properties() []prop.Media {
	// Declare we can produce 640x480, 30fps RGBA frames as an example
	supportedProp := prop.Media{
		Video: prop.Video{
			Width:  a.width,
			Height: a.height,
		},
	}
	return []prop.Media{supportedProp}
}
