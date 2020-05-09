package rtsp

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

type MulticastCommunicateInfo struct {
	AudioRtpPort    uint16
	CtlAudioRtpPort uint16
	VideoRtpPort    uint16
	CtlVideoRtpPort uint16

	AudioRtpMultiAddress    string
	CtlAudioRtpMultiAddress string
	VideoRtpMultiAddress    string
	CtlVideoRtpMultiAddress string

	SDPRaw          string
	Path            string
	SourceSessionId string
	SourceUrl       string
}

func (multiInfo *MulticastCommunicateInfo) String() string {
	return fmt.Sprintf("multicast.receiver[%s][%s][%s][%s:%d][%s:%d]", multiInfo.SourceSessionId, multiInfo.Path, multiInfo.SourceUrl,
		multiInfo.AudioRtpMultiAddress, multiInfo.AudioRtpPort, multiInfo.VideoRtpMultiAddress, multiInfo.VideoRtpPort)
}

type MulticastConn struct {
	SessionLogger
	*Pusher
	*Server
	multiInfo *MulticastCommunicateInfo

	AConn        *net.UDPConn
	AControlConn *net.UDPConn
	VConn        *net.UDPConn
	VControlConn *net.UDPConn

	StartAt   time.Time
	Stopped   bool
	OutBytes  int
	InBytes   int
	TransType TransType
	AControl  string
	ACodec    string
	VControl  string
	VCodec    string
}

func StartMulticastListen(pusher *Pusher, multiInfo *MulticastCommunicateInfo) (multiConn *MulticastConn, err error) {
	server := GetServer()
	multiConn = &MulticastConn{
		SessionLogger: SessionLogger{log.New(os.Stdout, "[RTSPServer]", log.LstdFlags|log.Lshortfile)},
		Server:        server,
		Pusher:        pusher,
		multiInfo:     multiInfo,

		StartAt:   time.Now(),
		Stopped:   false,
		OutBytes:  0,
		InBytes:   0,
		TransType: TRANS_TYPE_UDP,
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

	if multiInfo.AudioRtpPort != 0 && multiInfo.AudioRtpMultiAddress != "" {
		conn, err := multiConn.doMulticastListen(multiInfo.AudioRtpPort, multiInfo.AudioRtpMultiAddress, RTP_TYPE_AUDIO)
		if err != nil {
			return
		}
		multiConn.AConn = conn
	}
	if multiInfo.CtlAudioRtpPort != 0 && multiInfo.CtlAudioRtpMultiAddress != "" {
		conn, err := multiConn.doMulticastListen(multiInfo.CtlAudioRtpPort, multiInfo.CtlAudioRtpMultiAddress, RTP_TYPE_AUDIOCONTROL)
		if err != nil {
			return
		}
		multiConn.AControlConn = conn
	}
	if multiInfo.VideoRtpPort != 0 && multiInfo.VideoRtpMultiAddress != "" {
		conn, err := multiConn.doMulticastListen(multiInfo.VideoRtpPort, multiInfo.VideoRtpMultiAddress, RTP_TYPE_VIDEO)
		if err != nil {
			return
		}
		multiConn.VConn = conn
	}
	if multiInfo.CtlVideoRtpPort != 0 && multiInfo.CtlVideoRtpMultiAddress != "" {
		conn, err := multiConn.doMulticastListen(multiInfo.CtlVideoRtpPort, multiInfo.CtlVideoRtpMultiAddress, RTP_TYPE_VIDEOCONTROL)
		if err != nil {
			return
		}
		multiConn.VControlConn = conn
	}
	return
}

func (multiConn *MulticastConn) AddInputBytes(inputLength int) {
	multiConn.InBytes += inputLength
}

func (multiConn *MulticastConn) HandleRTP(pack *RTPPack) {
	multiConn.Pusher.queue <- pack
}

func (multiConn *MulticastConn) doMulticastListen(port uint16, multiAddr string, rType RTPType) (conn *net.UDPConn, err error) {

	multiUdpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprint(multiAddr, ":", port))
	if err != nil {
		multiConn.logger.Printf("multicast[%s:%d] conn set write buffer error, %v", multiAddr, port, err)
		return
	}
	conn, err = net.ListenMulticastUDP("udp", nil, multiUdpAddr)
	if err != nil {
		multiConn.logger.Printf("multicast[%s:%d] conn set write buffer error, %v", multiAddr, port, err)
		return
	}

	go func() {
		logger := multiConn.logger
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("multicast start listen address port[%s:%d]", multiAddr, port)
		defer logger.Printf("multicast stop listen address port[%s:%d]", multiAddr, port)
		timer := time.Unix(0, 0)
		for !multiConn.Stopped {
			if n, _, err := conn.ReadFromUDP(bufUDP); err == nil {
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

//TODO 初始化组播数据接收，新增pusher、结束pusher
