package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"
)

// FileOutputMediaStream saves media frames to local files
type FileOutputMediaStream struct {
	outputDir string
	mutex     sync.Mutex
	writers   map[string]*FileWriter
}

// Implement the required stream interface methods
func (s *FileOutputMediaStream) WriteRTPPacket(media *description.Media, forma format.Format, pkt *rtp.Packet, ntp time.Time, pts time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create writer for this track if not exists
	trackID := fmt.Sprintf("%s-%s", media.Type, forma.String())
	writer, ok := s.writers[trackID]
	if !ok {
		writer = NewFileWriter(s.outputDir, trackID, media.Type, forma)
		if s.writers == nil {
			s.writers = make(map[string]*FileWriter)
		}
		s.writers[trackID] = writer
	}

	// Process the packet and save frame if complete
	writer.ProcessPacket(pkt, pts)
}

// FileWriter handles the actual saving of frames
type FileWriter struct {
	// Implement fields for specific media type handling
	// including frame buffers, sample counters, etc.
}

func NewFileWriter(dir, id string, mediaType description.MediaType, format format.Format) *FileWriter {
	// Create appropriate writer based on media type and format
	// For video, create directories, set up image writers, etc.
}
