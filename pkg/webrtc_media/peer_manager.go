package webrtc_media

import (
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
)

type WebRTCPeer struct {
	ID         string
	PC         *webrtc.PeerConnection
	VideoTrack *webrtc.TrackLocalStaticSample
	// AudioTrack *webrtc.TrackLocalStaticRTP
	stop chan int
	pli  chan int
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
	if p == nil {
		return
	}
	if p.PC != nil {
		p.PC.Close()
	}
	close(p.stop)
	close(p.pli)
}

func (p *WebRTCPeer) AnswerSender(offer webrtc.SessionDescription) (answer webrtc.SessionDescription, err error) {
	fmt.Println("WebRTCPeer.AnswerSender")
	return webrtcEngine.CreateSenderReciverClient(offer, &p.PC, &p.VideoTrack, p.stop)
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
