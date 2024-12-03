package pkg

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"github.com/xlab/libvpx-go/vpx"
	"golang.org/x/image/draw"
)

type VCodec string

var (
	VideoBuilderClient = samplebuilder.New(200, &codecs.VP8Packet{}, 90000)
	frameResultChan    = make(chan *media.Sample)
)

const (
	CodecVP8 VCodec = "V_VP8"
	CodecVP9 VCodec = "V_VP9"
)

type VDecoder struct {
	enabled bool

	src   chan<- *image.RGBA
	ctx   *vpx.CodecCtx
	iface *vpx.CodecIface
	codec VCodec
}

func NewVDecoder(codec VCodec, src chan<- *image.RGBA) *VDecoder {
	dec := &VDecoder{
		src: src,
		ctx: vpx.NewCodecCtx(),
	}
	switch codec {
	case CodecVP8:
		dec.iface = vpx.DecoderIfaceVP8()
	case CodecVP9:
		dec.iface = vpx.DecoderIfaceVP9()
	default: // others are currently disabled
		log.Println("[WARN] unsupported VPX codec:", codec)
		return dec
	}
	err := vpx.Error(vpx.CodecDecInitVer(dec.ctx, dec.iface, nil, 0, vpx.DecoderABIVersion))
	if err != nil {
		log.Println("[WARN]", err)
		return dec
	}
	dec.enabled = true

	return dec
}

func (v *VDecoder) Save(savePath string) { //, out chan<- Frame
	//  defer close(out)
	i := 0

	for sample := range frameResultChan {

		dataSize := uint32(len(sample.Data))

		err := vpx.Error(vpx.CodecDecode(v.ctx, string(sample.Data), dataSize, nil, 0))
		if err != nil {
			log.Println("[WARN] --", err)
			continue
		}

		var iter vpx.CodecIter
		img := vpx.CodecGetFrame(v.ctx, &iter)
		if img != nil {
			img.Deref()

			// saves image locally to disk
			baseImg := YcbcrToRGBA(img.ImageYCbCr())
			wmf, err := os.Open("watermark.png")
			if err != nil {
				fmt.Printf("Failed to open watermark: %s\n", err)
			}
			defer wmf.Close()

			wm, err := png.Decode(wmf)
			if err != nil {
				fmt.Printf("Failed to  decode watermark: %s\n", err)
			}

			draw.Draw(baseImg, wm.Bounds().Add(image.Pt(0, 0)), wm, image.Point{}, draw.Over)

			// i++
			// buffer := new(bytes.Buffer)

			// if err = jpeg.Encode(buffer, baseImg, nil); err != nil {
			// 	//  panic(err)
			// 	fmt.Printf("jpeg Encode Error: %s\r\n", err)
			// }

			// fo, err := os.Create(fmt.Sprintf("%s%d%s", savePath, i%6, ".jpg"))
			// if err != nil {
			// 	fmt.Printf("image create Error: %s\r\n", err)
			// 	//panic(err)
			// }
			// // close fo on exit and check for its returned error
			// defer func() {
			// 	if err := fo.Close(); err != nil {
			// 		panic(err)
			// 	}
			// }()

			// if _, err := fo.Write(buffer.Bytes()); err != nil {
			// 	fmt.Printf("image write Error: %s\r\n", err)
			// 	//panic(err)
			// }
			// wait until the photo is saved to mimic the time it takes for the model the generate the frame

			// fo.Close()
			v.src <- baseImg
		}
	}
}
func ConvertRGBAtoYCbCr(src *image.RGBA) *image.YCbCr {
	rectangle := src.Bounds()
	img := image.NewYCbCr(rectangle, image.YCbCrSubsampleRatio420)
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y += 1 {
		for x := rectangle.Min.X; x < rectangle.Max.X; x += 1 {
			rgba := src.RGBAAt(x, y)
			yy, uu, vv := color.RGBToYCbCr(rgba.R, rgba.G, rgba.B)

			cy := img.YOffset(x, y)
			ci := img.COffset(x, y)
			img.Y[cy] = yy
			img.Cb[ci] = uu
			img.Cr[ci] = vv
		}
	}
	return img
}

func PushVPPacket(rtpPacket *rtp.Packet) {
	VideoBuilderClient.Push(rtpPacket)

	for {
		sample := VideoBuilderClient.Pop()

		if sample == nil {
			return
		}
		// Read VP8 header.
		videoKeyframe := (sample.Data[0]&0x1 == 0)

		if videoKeyframe {
			// Keyframe has frame information.
			raw := uint(sample.Data[6]) | uint(sample.Data[7])<<8 | uint(sample.Data[8])<<16 | uint(sample.Data[9])<<24
			width := int(raw & 0x3FFF)
			height := int((raw >> 16) & 0x3FFF)
			fmt.Println("VP8 keyframe", width, height)
		}
		frameResultChan <- sample
	}
}

func YcbcrToRGBA(ycbcr *image.YCbCr) *image.RGBA {
	bounds := ycbcr.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, ycbcr, image.Point{}, draw.Src)
	return rgba
}
