package webrtc_media

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"os"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/xlab/libvpx-go/vpx"
)

func DecodeRawFrame(sampleChan <-chan *media.Sample, imageChan chan *image.RGBA) {
	ctx := vpx.NewCodecCtx()
	switch CurrentVersion {
	case Version8:
		iface := vpx.DecoderIfaceVP8()
		err := vpx.Error(vpx.CodecDecInitVer(ctx, iface, nil, 0, vpx.DecoderABIVersion))
		if err != nil {
			log.Println("[WARN] ------------------------", err)
			return
		}
	case Version9:
		iface := vpx.DecoderIfaceVP9()
		err := vpx.Error(vpx.CodecDecInitVer(ctx, iface, nil, 0, vpx.DecoderABIVersion))

		if err != nil {
			log.Println("[WARN] ------------------------", err)
			return
		}
	}

	i := 0

	go func() {
		wmf, err := os.Open("watermark.png")
		if err != nil {
			fmt.Printf("Failed to open watermark: %s\n", err)
		}
		defer wmf.Close()

		wm, err := png.Decode(wmf)
		for sample := range sampleChan {
			dataSize := uint32(len(sample.Data))
			err := vpx.Error(vpx.CodecDecode(ctx, string(sample.Data), dataSize, nil, 0))
			if err != nil {
				log.Println("[WARN]", err)
				continue
			}

			var iter vpx.CodecIter
			img := vpx.CodecGetFrame(ctx, &iter)
			if img != nil {
				img.Deref()
				fmt.Println("Decoded frame")
				if img == nil {
					// No frame produced yet, decoder might need more data
					continue
				}
				i++
				if err != nil {
					fmt.Printf("Failed to  decode watermark: %s\n", err)
				}
				baseImg := YcbcrToRGBA(img.ImageYCbCr())

				draw.Draw(baseImg, wm.Bounds().Add(image.Pt(0, 0)), wm, image.Point{}, draw.Over)

				buffer := new(bytes.Buffer)

				if err = jpeg.Encode(buffer, baseImg, nil); err != nil {
					//  panic(err)
					fmt.Printf("jpeg Encode Error: %s\r\n", err)
				}

				fo, err := os.Create(fmt.Sprintf("%s%d%s", "output/", i%90, ".jpg"))
				fmt.Println("Saving frame to disk")
				if err != nil {
					fmt.Printf("image create Error: %s\r\n", err)
					//panic(err)
				}
				// close fo on exit and check for its returned error
				defer func() {
					if err := fo.Close(); err != nil {
						panic(err)
					}
				}()

				if _, err := fo.Write(buffer.Bytes()); err != nil {
					fmt.Printf("image write Error: %s\r\n", err)
					//panic(err)
				}
				// wait until the photo is saved to mimic the time it takes for the model the generate the frame

				fo.Close()
				imageChan <- baseImg
			}
		}

	}()
}

func YcbcrToRGBA(ycbcr *image.YCbCr) *image.RGBA {
	bounds := ycbcr.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, ycbcr, image.Point{}, draw.Src)
	return rgba
}
