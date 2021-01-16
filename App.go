package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/decima/go-socket.io-client"
	"github.com/pion/webrtc/v3"
	"log"
	"os"
	"sync"
	"time"
)

type MediaStatus struct {
	Id     string `json:"id"`
	Status bool   `json:"status"`
}
type SDPFormat struct {
	SDP *webrtc.SessionDescription `json:"sdp"`
}

func (sdp SDPFormat) toJSON() string {
	m, _ := json.Marshal(sdp)
	return string(m)
}

func NewSDPFormat(SDP webrtc.SessionDescription) *SDPFormat {
	return &SDPFormat{SDP: &SDP}
}

type ICECandidateFormat struct {
	ICE *webrtc.ICECandidateInit `json:"ice"`
}

func (ice ICECandidateFormat) toJSON() string {
	m, _ := json.Marshal(ice)
	return string(m)
}
func NewICECandidateFormat(ICE webrtc.ICECandidateInit) *ICECandidateFormat {
	return &ICECandidateFormat{ICE: &ICE}
}

var PeersLockers = sync.Mutex{}
var Peers = map[string]*webrtc.PeerConnection{}
var Client *socketio_client.Client
var VideoTrack = startVideo()

func main() {

	opts := &socketio_client.Options{
		Transport: "websocket",
		Query:     make(map[string]string),
	}
	//opts.Query["user"] = "user"
	//opts.Query["pwd"] = "pass"
	uri := "https://topaz.h91.co/socket.io/"

	Client, err := socketio_client.NewClient(uri, opts)
	if err != nil {
		log.Printf("NewClient error:%v\n", err)
		return
	}
	go func() {
		for {
			/*log.Println("waiting for next video")
			time.Sleep(6 * time.Second)

			VideoTrack = startVideo()
			*/
			log.Println(Client.Conn.Id())
			time.Sleep(5 * time.Second)
		}
	}()
	Client.On("error", func() {
		log.Printf("on error\n")
	})
	Client.On("connect", func() {
		log.Printf("on connect\n")

	})
	Client.On("message", func(id string, avatar interface{}, data map[string]interface{}) {

		log.Printf("%v (%v): %v\n", data["username"], id, data["message"])
	})

	Client.On("user-joined", func(id string, count int, clients []string, avatars map[string]interface{}) {
		log.Println(id, clients, count)
		for _, client := range clients {
			if Client.Conn.Id() != client {
				Peers[client] = DialClient(client)
			}

		}
	})

	Client.On("video-status-changed", func(data MediaStatus) {
		log.Println("video", data)
	})
	Client.On("sound-status-changed", func(data MediaStatus) {
		log.Println("audio", data)
	})
	Client.On("user-left", func(id string) {
		PeersLockers.Lock()
		if peer, ok := Peers[id]; ok {
			peer.Close()
			delete(Peers, id)
		}
		PeersLockers.Unlock()
	})

	Client.On("signal", gotMessageFromServer)

	Client.Emit("message", map[string]string{"username": "gopaz", "message": "JOINED"})

	reader := bufio.NewReader(os.Stdin)
	for {
		data, _, _ := reader.ReadLine()
		command := string(data)
		Client.Emit("message", map[string]string{"username": "gopaz", "message": command})
	}
}
func DialClient(clientId string) *webrtc.PeerConnection {
	p, ctx := NewPeer()
	<-ctx.Done()
	p.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		Client.Emit("signal", clientId, NewICECandidateFormat(candidate.ToJSON()).toJSON())
	})
	offer, _ := p.CreateOffer(nil)
	gatherComplete := webrtc.GatheringCompletePromise(p)
	<-gatherComplete
	Client.Emit("signal", clientId, NewSDPFormat(offer))

	return p
}
func gotMessageFromServer(fromId string, message string) {
	log.Println("got Signal from Server")
	PeersLockers.Lock()
	if _, ok := Peers[fromId]; !ok {
		peer, ctx := NewPeer()
		Peers[fromId] = peer
		<-ctx.Done()

	}
	PeersLockers.Unlock()

	var ice ICECandidateFormat
	var sdp SDPFormat
	json.Unmarshal([]byte(message), &ice)
	json.Unmarshal([]byte(message), &sdp)
	if ice.ICE != nil {
		Peers[fromId].AddICECandidate(*ice.ICE)
	}

	if sdp.SDP != nil {
		Peers[fromId].SetRemoteDescription(*sdp.SDP)
		answer, _ := Peers[fromId].CreateAnswer(nil)
		gatherComplete := webrtc.GatheringCompletePromise(Peers[fromId])
		<-gatherComplete
		Peers[fromId].SetLocalDescription(answer)
		Client.Emit("signal", fromId, NewSDPFormat(answer))
	}

}

func NewPeer() (*webrtc.PeerConnection, context.Context) {

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.services.mozilla.com"}},
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})

	if err != nil {
		panic(err)
	}

	context, iceConnectedCtxCancel := context.WithCancel(context.Background())

	rtpSender, _ := peerConnection.AddTrack(VideoTrack)

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateConnected {
			iceConnectedCtxCancel()
		}
	})
	return peerConnection, context
	/**offer := webrtc.SessionDescription{}

	log.Println(offer)
	//	signal.Decode(signal.MustReadStdin(), &offer)

	// Set the remote SessionDescription
	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}
	//peerConnection.AddICECandidate()

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	// Output the answer in base64 so we can paste it in browser
	fmt.Println(*peerConnection.LocalDescription())

	return nil**/
}
