package pkg

import (
	"fmt"
	"image"
	"io"
	"time"

	"github.com/pion/mediadevices/pkg/driver"
	"github.com/pion/mediadevices/pkg/frame"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
	"golang.org/x/exp/rand"
)

func InitMediaTracker(imgChan <-chan *image.RGBA, i int) {
	adapter := newResponseMediaAdapter()
	fmt.Println("adapter", i)
	adapter.imgChan = imgChan
	driver.GetManager().Register(adapter, driver.Info{
		Label:      fmt.Sprintf("ResponseMediaAdapter-%d", i),
		DeviceType: driver.Camera,
		Priority:   driver.PriorityHigh,
	})
}

type ResponseMediaAdapter struct {
	lastFrame *image.RGBA
	doneCh    chan struct{}
	imgChan   <-chan *image.RGBA
	tick      *time.Ticker
}

func newResponseMediaAdapter() *ResponseMediaAdapter {
	return &ResponseMediaAdapter{}
}

func (a *ResponseMediaAdapter) Open() error {
	a.doneCh = make(chan struct{})
	return nil
}

func (a *ResponseMediaAdapter) Close() error {
	close(a.doneCh)
	return nil
}
func (d *ResponseMediaAdapter) VideoRecord(p prop.Media) (video.Reader, error) {
	colors := [][3]byte{
		{235, 128, 128},
		{210, 16, 146},
		{170, 166, 16},
		{145, 54, 34},
		{107, 202, 222},
		{82, 90, 240},
		{41, 240, 110},
	}

	yi := p.Width * p.Height
	ci := yi / 2
	yy := make([]byte, yi)
	cb := make([]byte, ci)
	cr := make([]byte, ci)
	yyBase := make([]byte, yi)
	cbBase := make([]byte, ci)
	crBase := make([]byte, ci)
	hColorBarEnd := p.Height * 3 / 4
	wGradationEnd := p.Width * 5 / 7
	for y := 0; y < hColorBarEnd; y++ {
		yi := p.Width * y
		ci := p.Width * y / 2
		// Color bar
		for x := 0; x < p.Width; x++ {
			c := x * 7 / p.Width
			yyBase[yi+x] = uint8(uint16(colors[c][0]) * 75 / 100)
			cbBase[ci+x/2] = colors[c][1]
			crBase[ci+x/2] = colors[c][2]
		}
	}
	for y := hColorBarEnd; y < p.Height; y++ {
		yi := p.Width * y
		ci := p.Width * y / 2
		for x := 0; x < wGradationEnd; x++ {
			// Gray gradation
			yyBase[yi+x] = uint8(x * 255 / wGradationEnd)
			cbBase[ci+x/2] = 128
			crBase[ci+x/2] = 128
		}
		for x := wGradationEnd; x < p.Width; x++ {
			// Noise area
			cbBase[ci+x/2] = 128
			crBase[ci+x/2] = 128
		}
	}
	random := rand.New(rand.NewSource(0))

	tick := time.NewTicker(time.Duration(float32(time.Second) / p.FrameRate))
	d.tick = tick
	closed := d.doneCh

	r := video.ReaderFunc(func() (image.Image, func(), error) {
		select {
		case <-closed:
			return nil, func() {}, io.EOF
		default:
		}

		<-tick.C

		copy(yy, yyBase)
		copy(cb, cbBase)
		copy(cr, crBase)
		for y := hColorBarEnd; y < p.Height; y++ {
			yi := p.Width * y
			for x := wGradationEnd; x < p.Width; x++ {
				// Noise
				yy[yi+x] = uint8(random.Int31n(2) * 255)
			}
		}
		return &image.YCbCr{
			Y:              yy,
			YStride:        p.Width,
			Cb:             cb,
			Cr:             cr,
			CStride:        p.Width / 2,
			SubsampleRatio: image.YCbCrSubsampleRatio422,
			Rect:           image.Rect(0, 0, p.Width, p.Height),
		}, func() {}, nil
	})

	return r, nil
}

func (d ResponseMediaAdapter) Properties() []prop.Media {
	supportedProp := prop.Media{
		Video: prop.Video{
			Width:       640,
			Height:      480,
			FrameFormat: frame.FormatRGBA,
			FrameRate:   30,
		},
	}
	return []prop.Media{supportedProp}
}
