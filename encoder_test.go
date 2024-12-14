package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"log"
	"os"
	"testing"
	"time"

	wm "webrtc_poc_go/pkg/webrtc_media"

	"github.com/pion/webrtc/v4/pkg/media"
)

type MockTrackLocalStaticSample struct {
	writtenSamples []media.Sample
}

func (m *MockTrackLocalStaticSample) WriteSample(sample media.Sample) error {
	log.Println("MockTrackLocalStaticSample.WriteSample called", sample.Data)
	// This method will be called instead of the original TrackLocalStaticSample method
	m.writtenSamples = append(m.writtenSamples, sample)
	return nil
}

func TestInitEncoderFrameSender(t *testing.T) {
	// Load a JPEG file from output/0.jpeg
	jpegData, err := os.ReadFile("./output/received_image_0.jpg")
	if err != nil {
		t.Fatalf("Failed to read JPEG file: %v", err)
	}

	fmt.Println("Read JPEG file", len(jpegData))

	// Verify that the file is a valid JPEG
	_, err = jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("Failed to decode JPEG: %v", err)
	}

	// Create a mock video track
	// codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9} // Adjust codec as needed
	mockSampleTrack := &MockTrackLocalStaticSample{}
	// Create the channel and start the sender
	receiverChan := make(chan []byte)

	// We call the function under test
	wm.InitEncoderFrameSender(mockSampleTrack, receiverChan)

	// Send the JPEG data into the channel and then close it
	receiverChan <- jpegData
	close(receiverChan)

	// The function under test runs in a goroutine. Give it a moment to process.
	time.Sleep(3 * time.Second)

	// Check that a sample was written to the track
	if len(mockSampleTrack.writtenSamples) == 0 {
		t.Fatal("No samples were written to the mock track")
	}

	// Verify the sample data is not empty
	sample := mockSampleTrack.writtenSamples[0]
	if len(sample.Data) == 0 {
		t.Error("Written sample data is empty, expected encoded frame")
	}

	// Optionally check the duration
	if sample.Duration <= 0 {
		t.Errorf("Duration of the sample should be > 0, got %v", sample.Duration)
	}

	log.Println("TestInitEncoderFrameSender passed")
}
