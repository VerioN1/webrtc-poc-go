package webrtc_media

import (
	"log"

	grpc_service "webrtc_poc_go/pkg/grpc_server"

	"image"
	"image/draw"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/xlab/libvpx-go/vpx"
)

func DecodeVPAndWriteYUV(sampleChan <-chan *media.Sample, peerID string) {
	ctx := vpx.NewCodecCtx()
	iface := vpx.DecoderIfaceVP8()
	grpcInstnc := grpc_service.GetConnectionManager().GetConnection(peerID)

	// i := 0
	err := vpx.Error(vpx.CodecDecInitVer(ctx, iface, nil, 0, vpx.DecoderABIVersion))
	if err != nil {
		log.Println("[WARN] ------------------------", err)
		return
	}

	frameChan := make(chan []byte)

	// Start a goroutine to send frames to the server
	go func() {
		for frameData := range frameChan {
			// Move StreamImage call here so decoding isn't blocked by Send()
			if grpcInstnc != nil {
				grpcInstnc.ReceiverChan <- frameData
			}
		}
	}()

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
			if img == nil {
				// No frame produced yet, decoder might need more data
				continue
			}
			rgba := img.ImageRGBA()
			if rgba != nil {
				frameChan <- rgba.Pix
			}
		}
	}
	close(frameChan)
}

// AddWatermark overlays a watermark on an image
func (w *WebRTCEngine) AddWatermark(img image.Image) image.Image {
	if w.watermark == nil {
		return img // No watermark available
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)

	// Draw the original image
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	// Calculate position for the watermark (bottom right corner)
	wmBounds := w.watermark.Bounds()
	x := bounds.Max.X - wmBounds.Dx() - 10 // 10px padding
	y := bounds.Max.Y - wmBounds.Dy() - 10 // 10px padding
	offset := image.Pt(x, y)

	// Draw the watermark
	draw.Draw(rgba, wmBounds.Add(offset), w.watermark, wmBounds.Min, draw.Over)

	return rgba
}
