package webrtc_media

import (
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/rtp/codecs/av1/obu"
)

// FrameData contains a complete video frame
type FrameData struct {
	Data      []byte
	Timestamp uint64
}

// FrameHeader contains IVF frame header information
type FrameHeader struct {
	Length    uint32
	Timestamp uint64
}

var (
	errFileNotOpened        = errors.New("file not opened")
	errInvalidNilPacket     = errors.New("invalid nil packet")
	errCodecUnset           = errors.New("codec is unset")
	errCodecAlreadySet      = errors.New("codec is already set")
	errNoSuchCodec          = errors.New("no codec for this MimeType")
	errInvalidMediaTimebase = errors.New("invalid media timebase")
)

type (
	codecc int

	// IVFWriter is used to take RTP packets and write them to an IVF on disk.
	IVFWriter struct {
		ioWriter     io.Writer
		count        uint64
		seenKeyFrame bool

		// Channel for frames
		frameCh chan FrameData

		codec codecc

		timebaseDenominator uint32
		timebaseNumerator   uint32
		firstFrameTimestamp uint32
		clockRate           uint64

		// VP8, VP9
		currentFrame []byte

		// AV1
		av1Depacketizer *codecs.AV1Depacketizer
	}
)

const (
	codecUnset codecc = iota
	codecVP8
	codecVP9
	codecAV1

	mimeTypeVP8 = "video/VP8"
	mimeTypeVP9 = "video/VP9"
	mimeTypeAV1 = "video/AV1"
)

// New builds a new IVF writer.
func New(fileName string, opts ...Option) (*IVFWriter, error) {
	file, err := os.Create(fileName) //nolint:gosec
	if err != nil {
		return nil, err
	}
	writer, err := NewWith(file, opts...)
	if err != nil {
		return nil, err
	}
	writer.ioWriter = file

	return writer, nil
}

// NewWithChannels initializes a new IVF writer that outputs to channels instead of a file
func NewWithChannels(frameCh chan FrameData, opts ...Option) (*IVFWriter, error) {
	writer := &IVFWriter{
		ioWriter:            io.Discard, // Use discard for file header
		seenKeyFrame:        false,
		timebaseDenominator: 30,
		timebaseNumerator:   1,
		clockRate:           90000,
		frameCh:             frameCh,
	}

	for _, o := range opts {
		if err := o(writer); err != nil {
			return nil, err
		}
	}

	if writer.codec == codecUnset {
		writer.codec = codecVP8
	}
	if err := writer.writeHeader(); err != nil {
		return nil, err
	}

	if writer.timebaseDenominator == 0 {
		return nil, errInvalidMediaTimebase
	}

	return writer, nil
}

// NewWith initialize a new IVF writer with an io.Writer output.
func NewWith(out io.Writer, opts ...Option) (*IVFWriter, error) {
	if out == nil {
		return nil, errFileNotOpened
	}

	writer := &IVFWriter{
		ioWriter:            out,
		seenKeyFrame:        false,
		timebaseDenominator: 30,
		timebaseNumerator:   1,
		clockRate:           90000,
	}

	for _, o := range opts {
		if err := o(writer); err != nil {
			return nil, err
		}
	}

	if writer.codec == codecUnset {
		writer.codec = codecVP8
	}
	if err := writer.writeHeader(); err != nil {
		return nil, err
	}

	if writer.timebaseDenominator == 0 {
		return nil, errInvalidMediaTimebase
	}

	return writer, nil
}

func (i *IVFWriter) writeHeader() error {
	header := make([]byte, 32)
	copy(header[0:], "DKIF")                      // DKIF
	binary.LittleEndian.PutUint16(header[4:], 0)  // Version
	binary.LittleEndian.PutUint16(header[6:], 32) // Header size

	// FOURCC
	switch i.codec {
	case codecVP8:
		copy(header[8:], "VP80")
	case codecVP9:
		copy(header[8:], "VP90")
	case codecAV1:
		copy(header[8:], "AV01")
	default:
		return errCodecUnset
	}

	binary.LittleEndian.PutUint16(header[12:], 640)                   // Width in pixels
	binary.LittleEndian.PutUint16(header[14:], 480)                   // Height in pixels
	binary.LittleEndian.PutUint32(header[16:], i.timebaseDenominator) // Framerate denominator
	binary.LittleEndian.PutUint32(header[20:], i.timebaseNumerator)   // Framerate numerator
	binary.LittleEndian.PutUint32(header[24:], 900)                   // Frame count, will be updated on first Close() call
	binary.LittleEndian.PutUint32(header[28:], 0)                     // Unused

	_, err := i.ioWriter.Write(header)

	return err
}

func (i *IVFWriter) timestampToPts(timestamp uint64) uint64 {
	return timestamp * uint64(i.timebaseNumerator) / uint64(i.timebaseDenominator)
}

func (i *IVFWriter) writeFrame(frame []byte, timestamp uint64) error {
	// frameLength := uint32(len(frame))
	// ptsTimestamp := i.timestampToPts(timestamp)
	i.count++

	// If channel is available, send frame data
	if i.frameCh != nil {
		// Create a copy of the frame data to avoid issues with reuse
		frameCopy := make([]byte, len(frame))
		copy(frameCopy, frame)

		i.frameCh <- FrameData{
			Data:      frameCopy,
			Timestamp: timestamp,
		}
		return nil
	}

	// Otherwise fall back to writing to file
	_, err := i.ioWriter.Write(frame)
	return err
}

// WriteRTP adds a new packet and writes the appropriate headers for it.
func (i *IVFWriter) WriteRTP(packet *rtp.Packet) error {
	if i.ioWriter == nil && i.frameCh == nil {
		return errFileNotOpened
	} else if len(packet.Payload) == 0 {
		return nil
	}

	if i.count == 0 {
		i.firstFrameTimestamp = packet.Timestamp
	}
	relativeTstampMs := 1000 * uint64(packet.Timestamp-i.firstFrameTimestamp) / i.clockRate

	switch i.codec {
	case codecVP8:
		return i.writeVP8(packet, relativeTstampMs)
	case codecVP9:
		return i.writeVP9(packet, relativeTstampMs)
	case codecAV1:
		return i.writeAV1(packet, relativeTstampMs)
	default:
		return errCodecUnset
	}
}

func (i *IVFWriter) writeVP8(packet *rtp.Packet, timestamp uint64) error {
	vp8Packet := codecs.VP8Packet{}
	if _, err := vp8Packet.Unmarshal(packet.Payload); err != nil {
		return err
	}

	isKeyFrame := (vp8Packet.Payload[0] & 0x01) == 0
	switch {
	case !i.seenKeyFrame && !isKeyFrame:
		return nil
	case i.currentFrame == nil && vp8Packet.S != 1:
		return nil
	}

	i.seenKeyFrame = true
	i.currentFrame = append(i.currentFrame, vp8Packet.Payload[0:]...)

	if !packet.Marker {
		return nil
	} else if len(i.currentFrame) == 0 {
		return nil
	}

	if err := i.writeFrame(i.currentFrame, timestamp); err != nil {
		return err
	}
	i.currentFrame = nil

	return nil
}

func (i *IVFWriter) writeVP9(packet *rtp.Packet, timestamp uint64) error {
	vp9Packet := codecs.VP9Packet{}
	if _, err := vp9Packet.Unmarshal(packet.Payload); err != nil {
		return err
	}

	switch {
	case !i.seenKeyFrame && vp9Packet.P:
		return nil
	case i.currentFrame == nil && !vp9Packet.B:
		return nil
	}

	i.seenKeyFrame = true
	i.currentFrame = append(i.currentFrame, vp9Packet.Payload[0:]...)

	if !packet.Marker {
		return nil
	} else if len(i.currentFrame) == 0 {
		return nil
	}

	// the timestamp must be sequential. webrtc mandates a clock rate of 90000
	// and we've assumed 30fps in the header.
	if err := i.writeFrame(i.currentFrame, timestamp); err != nil {
		return err
	}
	i.currentFrame = nil

	return nil
}

func (i *IVFWriter) writeAV1(packet *rtp.Packet, timestamp uint64) error {
	if i.av1Depacketizer == nil {
		i.av1Depacketizer = &codecs.AV1Depacketizer{}
	}

	payload, err := i.av1Depacketizer.Unmarshal(packet.Payload)
	if err != nil {
		return err
	}

	if !i.seenKeyFrame {
		isKeyFrame := i.av1Depacketizer.N || (len(payload) > 0 && obu.Type((payload[0]&0x78)>>3) == obu.OBUSequenceHeader)
		if !isKeyFrame {
			return nil
		}

		i.seenKeyFrame = true
	}

	i.currentFrame = append(i.currentFrame, payload...)
	if !packet.Marker {
		return nil
	}

	delimiter := obu.Header{
		Type:         obu.OBUTemporalDelimiter,
		HasSizeField: true,
	}
	frame := append(delimiter.Marshal(), 0)
	frame = append(frame, i.currentFrame...)

	if err := i.writeFrame(frame, timestamp); err != nil {
		return err
	}
	i.currentFrame = nil

	return nil
}

// Close stops the recording.
func (i *IVFWriter) Close() error {
	if i.ioWriter == nil && i.frameCh == nil {
		// Returns no error as it may be convenient to call
		// Close() multiple times
		return nil
	}

	// Close channel if it exists
	if i.frameCh != nil {
		close(i.frameCh)
		i.frameCh = nil
	}

	// Handle file operations
	if i.ioWriter != nil {
		defer func() {
			i.ioWriter = nil
		}()

		if ws, ok := i.ioWriter.(io.WriteSeeker); ok {
			// Update the framecount
			if _, err := ws.Seek(24, 0); err != nil {
				return err
			}
			buff := make([]byte, 4)
			binary.LittleEndian.PutUint32(buff, uint32(i.count)) //nolint:gosec // G115
			if _, err := ws.Write(buff); err != nil {
				return err
			}
		}

		if closer, ok := i.ioWriter.(io.Closer); ok {
			return closer.Close()
		}
	}

	return nil
}

// An Option configures a SampleBuilder.
type Option func(i *IVFWriter) error

// WithCodec configures if IVFWriter is writing AV1 or VP8 packets to disk.
func WithCodec(mimeType string) Option {
	return func(i *IVFWriter) error {
		if i.codec != codecUnset {
			return errCodecAlreadySet
		}

		switch mimeType {
		case mimeTypeVP8:
			i.codec = codecVP8
		case mimeTypeVP9:
			i.codec = codecVP9
		case mimeTypeAV1:
			i.codec = codecAV1
		default:
			return errNoSuchCodec
		}

		return nil
	}
}
