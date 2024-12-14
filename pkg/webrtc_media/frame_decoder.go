package webrtc_media

import (
	"log"

	grpc_service "webrtc_poc_go/pkg/grpc_server"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/xlab/libvpx-go/vpx"
)

func DecodeVP9AndWriteYUV(sampleChan <-chan *media.Sample, grpcInstnc *grpc_service.GrpcServerManager) {
	ctx := vpx.NewCodecCtx()
	iface := vpx.DecoderIfaceVP8()
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
			grpcInstnc.StreamImage(frameData, true)
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
