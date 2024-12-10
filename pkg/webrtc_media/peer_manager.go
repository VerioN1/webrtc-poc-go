package webrtc_media

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type WebRTCPeer struct {
	ID         string
	PC         *webrtc.PeerConnection
	VideoTrack *webrtc.TrackLocalStaticSample
	AudioTrack *webrtc.TrackLocalStaticRTP
	stop       chan int
	pli        chan int
}

var (
	webrtcEngine *WebRTCEngine
)

type PeersManager struct {
	peers map[string]*WebRTCPeer
	mu    sync.RWMutex
}

func NewPeersManager() *PeersManager {
	return &PeersManager{
		peers: make(map[string]*WebRTCPeer),
		mu:    sync.RWMutex{},
	}
}

func init() {
	webrtcEngine = NewWebRTCEngine()
}
func NewWebRTCPeer(id string) *WebRTCPeer {
	return &WebRTCPeer{
		ID:   id,
		stop: make(chan int),
		pli:  make(chan int),
	}
}

func (p *WebRTCPeer) Stop() {
	p.PC.Close()
	close(p.stop)
	close(p.pli)
}

func (p *WebRTCPeer) AnswerSender(offer webrtc.SessionDescription) (answer webrtc.SessionDescription, err error) {
	fmt.Println("WebRTCPeer.AnswerSender")
	return webrtcEngine.CreateSenderReciverClient(offer, &p.PC, &p.VideoTrack, &p.AudioTrack, p.stop, p.pli)
}

func (p *WebRTCPeer) SendPLI() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("%v", r)
				return
			}
		}()
		ticker := time.NewTicker(time.Second)
		i := 0
		for {
			select {
			case <-ticker.C:
				p.pli <- 1
				if i > 3 {
					return
				}
				i++
			case <-p.stop:
				return
			}
		}
	}()
}

func (pm *PeersManager) AddPeer(id string, p *WebRTCPeer) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.peers[id] = p
}

func (pm *PeersManager) RemovePeer(id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.peers[id].Stop()
	delete(pm.peers, id)
}

// Retrieve a peer from the map
func (pm *PeersManager) GetPeer(id string) *WebRTCPeer {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.peers[id]
}
