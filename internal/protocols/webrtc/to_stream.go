package webrtc

import (
	"fmt"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/rtptime"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

	"webrtc_poc_go/internal/stream"
)

// ToStream maps a WebRTC connection to a MediaMTX stream.
func ToStream(incomingTracks []*IncomingTrack, stream **stream.Stream) ([]*description.Media, error) {
	var medias []*description.Media //nolint:prealloc
	timeDecoder := rtptime.NewGlobalDecoder()

	for _, track := range incomingTracks {
		var typ description.MediaType
		var forma format.Format

		switch strings.ToLower(track.Track.Codec().MimeType) {
		case strings.ToLower(webrtc.MimeTypeAV1):
			typ = description.MediaTypeVideo
			forma = &format.AV1{
				PayloadTyp: uint8(track.Track.PayloadType()),
			}

		case strings.ToLower(webrtc.MimeTypeVP9):
			typ = description.MediaTypeVideo
			forma = &format.VP9{
				PayloadTyp: uint8(track.Track.PayloadType()),
			}

		case strings.ToLower(webrtc.MimeTypeVP8):
			typ = description.MediaTypeVideo
			forma = &format.VP8{
				PayloadTyp: uint8(track.Track.PayloadType()),
			}

		case strings.ToLower(webrtc.MimeTypeH265):
			typ = description.MediaTypeVideo
			forma = &format.H265{
				PayloadTyp: uint8(track.Track.PayloadType()),
			}

		case strings.ToLower(webrtc.MimeTypeH264):
			typ = description.MediaTypeVideo
			forma = &format.H264{
				PayloadTyp:        uint8(track.Track.PayloadType()),
				PacketizationMode: 1,
			}

		case strings.ToLower(mimeTypeMultiopus):
			typ = description.MediaTypeAudio
			forma = &format.Opus{
				PayloadTyp:   uint8(track.Track.PayloadType()),
				ChannelCount: int(track.Track.Codec().Channels),
			}

		case strings.ToLower(webrtc.MimeTypeOpus):
			typ = description.MediaTypeAudio
			forma = &format.Opus{
				PayloadTyp: uint8(track.Track.PayloadType()),
				ChannelCount: func() int {
					if strings.Contains(track.Track.Codec().SDPFmtpLine, "stereo=1") {
						return 2
					}
					return 1
				}(),
			}

		case strings.ToLower(webrtc.MimeTypeG722):
			typ = description.MediaTypeAudio
			forma = &format.G722{}

		case strings.ToLower(webrtc.MimeTypePCMU):
			channels := int(track.Track.Codec().Channels)
			if channels == 0 {
				channels = 1
			}

			typ = description.MediaTypeAudio
			forma = &format.G711{
				PayloadTyp: func() uint8 {
					if channels > 1 {
						return 118
					}
					return 0
				}(),
				MULaw:        true,
				SampleRate:   8000,
				ChannelCount: channels,
			}

		case strings.ToLower(webrtc.MimeTypePCMA):
			channels := int(track.Track.Codec().Channels)
			if channels == 0 {
				channels = 1
			}

			typ = description.MediaTypeAudio
			forma = &format.G711{
				PayloadTyp: func() uint8 {
					if channels > 1 {
						return 119
					}
					return 8
				}(),
				MULaw:        false,
				SampleRate:   8000,
				ChannelCount: channels,
			}

		case strings.ToLower(mimeTypeL16):
			typ = description.MediaTypeAudio
			forma = &format.LPCM{
				PayloadTyp:   uint8(track.Track.PayloadType()),
				BitDepth:     16,
				SampleRate:   int(track.Track.Codec().ClockRate),
				ChannelCount: int(track.Track.Codec().Channels),
			}

		default:
			return nil, fmt.Errorf("unsupported codec: %+v", track.Track.Codec().RTPCodecCapability)
		}

		medi := &description.Media{
			Type:    typ,
			Formats: []format.Format{forma},
		}

		track.OnPacketRTP = func(pkt *rtp.Packet) {
			pts, ok := timeDecoder.Decode(track, pkt)
			if !ok {
				return
			}

			(*stream).WriteRTPPacket(medi, forma, pkt, time.Now(), pts)
		}

		medias = append(medias, medi)
	}

	return medias, nil
}
