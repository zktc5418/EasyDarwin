package rtsp

import (
	"bytes"
	"fmt"
	"github.com/bruce-qin/EasyGoLib/utils"
	"github.com/emirpasic/gods/sets/hashset"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

type MulticastCommunicateInfo struct {
	AudioRtpPort    uint16 `json:"aPort"`
	CtlAudioRtpPort uint16 `json:"caPort"`
	VideoRtpPort    uint16 `json:"vPort"`
	CtlVideoRtpPort uint16 `json:"cvPort"`

	AudioRtpMultiAddress    string `json:"aAddr"`
	CtlAudioRtpMultiAddress string `json:"caAddr"`
	VideoRtpMultiAddress    string `json:"vAddr"`
	CtlVideoRtpMultiAddress string `json:"cvAddr"`

	SDPRaw          string `json:"sdp"`
	Path            string `json:"path"`
	SourceSessionId string `json:"id"`
	SourceUrl       string `json:"url"`

	AudioStreamPort       uint16 `json:"audioStreamPort"`
	AudioMulticastAddress string `json:"audioMulticastAddress"`
	VideoStreamPort       uint16 `json:"videoStreamPort"`
	VideoMulticastAddress string `json:"videoMulticastAddress"`
}

var existMulticastAddresses = hashset.New()
var setLock = sync.RWMutex{}

func RandomMulticastAddress() (multicastAddr string, port uint16) {
	setLock.Lock()
	for {
		multicastAddr, port = utils.RandomMulticastAddress()
		fmtAddr := fmt.Sprint(multicastAddr, ":", port)
		if !existMulticastAddresses.Contains(fmtAddr) {
			existMulticastAddresses.Add(fmtAddr)
			setLock.Unlock()
			return
		}
	}
}
func AddExistMulticastAddress(multicastAddr string, port uint16) {
	setLock.Lock()
	existMulticastAddresses.Add(fmt.Sprint(multicastAddr, ":", port))
	setLock.Unlock()
}
func ReleaseMulticastAddress(multicastAddr string, port uint16) {
	setLock.Lock()
	existMulticastAddresses.Remove(fmt.Sprint(multicastAddr, ":", port))
	setLock.Unlock()
}

func (multiInfo *MulticastCommunicateInfo) String() string {
	return fmt.Sprintf("multicast.receiver[%s][%s][%s][%s:%d][%s:%d]", multiInfo.SourceSessionId, multiInfo.Path, multiInfo.SourceUrl,
		multiInfo.AudioRtpMultiAddress, multiInfo.AudioRtpPort, multiInfo.VideoRtpMultiAddress, multiInfo.VideoRtpPort)
}

type MulticastCommandValue int8

const (
	START_MULTICAST MulticastCommandValue = iota
	STOP_MULTICAST
)

type MulticastCommand struct {
	MultiInfo *MulticastCommunicateInfo `json:"mulInf"`
	Command   MulticastCommandValue     `json:"cmd"`
}

type MulticastClient struct {
	SessionLogger
	*Pusher
	*Server
	multiInfo *MulticastCommunicateInfo

	AConn        *ipv4.PacketConn
	AControlConn *ipv4.PacketConn
	VConn        *ipv4.PacketConn
	VControlConn *ipv4.PacketConn

	StartAt   time.Time
	Stopped   bool
	OutBytes  int
	InBytes   int
	TransType TransType
	AControl  string
	ACodec    string
	VControl  string
	VCodec    string

	RTPHandles  []func(*RTPPack)
	StopHandles []func()
}

func StartMulticastListen(pusher *Pusher, multiInfo *MulticastCommunicateInfo) (multiConn *MulticastClient, err error) {
	server := GetServer()
	multiConn = &MulticastClient{
		SessionLogger: SessionLogger{log.New(os.Stdout, "[RTSPServer]", log.LstdFlags|log.Lshortfile)},
		Server:        server,
		Pusher:        pusher,
		multiInfo:     multiInfo,

		StartAt:     time.Now(),
		Stopped:     false,
		OutBytes:    0,
		InBytes:     0,
		TransType:   TRANS_TYPE_UDP,
		RTPHandles:  make([]func(*RTPPack), 0),
		StopHandles: make([]func(), 0),
	}
	sdpMap := ParseSDP(multiInfo.SDPRaw)
	sdp, ok := sdpMap["audio"]
	if ok {
		multiConn.AControl = sdp.Control
		multiConn.ACodec = sdp.Codec
	}
	sdp, ok = sdpMap["video"]
	if ok {
		multiConn.VControl = sdp.Control
		multiConn.VCodec = sdp.Codec
	}
	var conn *ipv4.PacketConn
	if multiInfo.AudioRtpPort != 0 && multiInfo.AudioRtpMultiAddress != "" {
		conn, err = multiConn.doMulticastListen(multiInfo.AudioRtpPort, multiInfo.AudioRtpMultiAddress, RTP_TYPE_AUDIO)
		if err != nil {
			return
		}
		multiConn.AConn = conn
	}
	if multiInfo.CtlAudioRtpPort != 0 && multiInfo.CtlAudioRtpMultiAddress != "" {
		conn, err = multiConn.doMulticastListen(multiInfo.CtlAudioRtpPort, multiInfo.CtlAudioRtpMultiAddress, RTP_TYPE_AUDIOCONTROL)
		if err != nil {
			return
		}
		multiConn.AControlConn = conn
	}
	if multiInfo.VideoRtpPort != 0 && multiInfo.VideoRtpMultiAddress != "" {
		conn, err = multiConn.doMulticastListen(multiInfo.VideoRtpPort, multiInfo.VideoRtpMultiAddress, RTP_TYPE_VIDEO)
		if err != nil {
			return
		}
		multiConn.VConn = conn
	}
	if multiInfo.CtlVideoRtpPort != 0 && multiInfo.CtlVideoRtpMultiAddress != "" {
		conn, err = multiConn.doMulticastListen(multiInfo.CtlVideoRtpPort, multiInfo.CtlVideoRtpMultiAddress, RTP_TYPE_VIDEOCONTROL)
		if err != nil {
			return
		}
		multiConn.VControlConn = conn
	}
	return
}

func (multiConn *MulticastClient) AddInputBytes(inputLength int) {
	multiConn.InBytes += inputLength
}

func (multiConn *MulticastClient) HandleRTP(pack *RTPPack) {
	for _, h := range multiConn.RTPHandles {
		h(pack)
	}
}

func (multiConn *MulticastClient) Stop() {
	if multiConn.Stopped {
		return
	}
	multiConn.Stopped = true
	for _, h := range multiConn.StopHandles {
		h()
	}
	if multiConn.AConn != nil {
		_ = multiConn.AConn.Close()
		multiConn.AConn = nil
	}
	if multiConn.AControlConn != nil {
		_ = multiConn.AControlConn.Close()
		multiConn.AControlConn = nil
	}
	if multiConn.VConn != nil {
		_ = multiConn.VConn.Close()
		multiConn.VConn = nil
	}
	if multiConn.VControlConn != nil {
		_ = multiConn.VControlConn.Close()
		multiConn.VControlConn = nil
	}
}

func (multiConn *MulticastClient) doMulticastListen(port uint16, multiAddr string, rType RTPType) (conn *ipv4.PacketConn, err error) {

	multiUdpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprint(multiAddr, ":", port))
	if err != nil {
		multiConn.logger.Printf("multicast[%s:%d] conn set write buffer error, %v", multiAddr, port, err)
		return
	}
	conn, err = utils.ListenMulticastAddress(multiUdpAddr, multiConn.Server.multicastBindInf)

	//conn, err = net.ListenMulticastUDP("udp", multiConn.Server.multicastBindInf, multiUdpAddr)
	if err != nil {
		multiConn.logger.Printf("multicast[%s:%d] conn set write buffer error, %v", multiAddr, port, err)
		return
	}

	go func() {
		logger := multiConn.logger
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("multicast start listen address port[%s:%d]", multiAddr, port)
		defer logger.Printf("multicast stop listen address port[%s:%d]", multiAddr, port)
		defer ReleaseMulticastAddress(multiAddr, port)
		defer func() {
			if err := recover(); err != nil {
				logger.Printf("multicast rtsp packet error:%v", err)
			}
		}()
		AddExistMulticastAddress(multiAddr, port)
		timer := time.Unix(0, 0)
		for !multiConn.Stopped {
			if n, _, _, err := conn.ReadFrom(bufUDP); err == nil {
				elapsed := time.Now().Sub(timer)
				if elapsed >= 30*time.Second {
					logger.Printf("Package recv from multicast[%s:%d]::%d\n", multiAddr, port, n)
					timer = time.Now()
				}
				rtpBytes := make([]byte, n)
				multiConn.AddInputBytes(n)
				copy(rtpBytes, bufUDP)
				pack := &RTPPack{
					Type:   rType,
					Buffer: bytes.NewBuffer(rtpBytes),
				}
				multiConn.HandleRTP(pack)
			} else {
				logger.Printf("Package recv from multicast[%s:%d], %v", multiAddr, port, err)
				continue
			}
		}
	}()
	return
}
