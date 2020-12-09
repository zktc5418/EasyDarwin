package rtsp

import (
	"bytes"
	"fmt"
	"github.com/bruce-qin/EasyGoLib/utils"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"time"
)

type UDPServer struct {
	*Session
	*RTSPClient

	APort        int
	AConn        *ipv4.PacketConn
	AControlPort int
	AControlConn *ipv4.PacketConn
	VPort        int
	VConn        *ipv4.PacketConn
	VControlPort int
	VControlConn *ipv4.PacketConn

	Stoped bool
}

func (s *UDPServer) AddInputBytes(bytes int) {
	if s.Session != nil {
		s.Session.InBytes += bytes
		return
	}
	if s.RTSPClient != nil {
		s.RTSPClient.InBytes += bytes
		return
	}
	panic(fmt.Errorf("session and RTSPClient both nil"))
}

func (s *UDPServer) HandleRTP(pack *RTPPack) {
	if s.Session != nil {
		for _, v := range s.Session.RTPHandles {
			v(pack)
		}
		return
	}

	if s.RTSPClient != nil {
		for _, v := range s.RTSPClient.RTPHandles {
			v(pack)
		}
		return
	}
	panic(fmt.Errorf("session and RTSPClient both nil"))
}

func (s *UDPServer) Logger() *log.Logger {
	if s.Session != nil {
		return s.Session.logger
	}
	if s.RTSPClient != nil {
		return s.RTSPClient.logger
	}
	panic(fmt.Errorf("session and RTSPClient both nil"))
}

func (s *UDPServer) Stop() {
	if s.Stoped {
		return
	}
	s.Stoped = true
	if s.AConn != nil {
		s.AConn.Close()
		s.AConn = nil
	}
	if s.AControlConn != nil {
		s.AControlConn.Close()
		s.AControlConn = nil
	}
	if s.VConn != nil {
		s.VConn.Close()
		s.VConn = nil
	}
	if s.VControlConn != nil {
		s.VControlConn.Close()
		s.VControlConn = nil
	}
}

func (s *UDPServer) SetupAudio() (err error) {
	server := GetServer()
	logger := s.Logger()
	aport, err := utils.FindAvailableUDPPort(server.rtpMinUdpPort, server.rtpMaxUdpPort)
	if err != nil {
		return
	}
	aConn, err := net.ListenPacket("udp4", fmt.Sprint(":", aport))
	if err != nil {
		logger.Printf("udp server audio conn set listen error, %v", err)
		return
	}
	s.AConn = ipv4.NewPacketConn(aConn)
	s.APort = int(aport)
	go func() {
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("udp server start listen audio port[%d]", s.APort)
		defer logger.Printf("udp server stop listen audio port[%d]", s.APort)
		timer := time.Unix(0, 0)
		for !s.Stoped {
			if n, _, _, err := s.AConn.ReadFrom(bufUDP); err == nil {
				elapsed := time.Now().Sub(timer)
				if elapsed >= 30*time.Second {
					logger.Printf("Package recv from AConn.len:%d\n", n)
					timer = time.Now()
				}
				rtpBytes := make([]byte, n)
				s.AddInputBytes(n)
				copy(rtpBytes, bufUDP)
				pack := &RTPPack{
					Type:   RTP_TYPE_AUDIO,
					Buffer: bytes.NewBuffer(rtpBytes),
				}
				s.HandleRTP(pack)
			} else {
				logger.Println("udp server read audio pack error", err)
				continue
			}
		}
	}()
	cport, err := utils.FindAvailableUDPPort(server.rtpMinUdpPort, server.rtpMaxUdpPort)
	if err != nil {
		return
	}
	aControlConn, err := net.ListenPacket("udp4", fmt.Sprint(":", cport))
	if err != nil {
		logger.Printf("udp server audio control conn set listen error, %v", err)
		return
	}
	s.AControlConn = ipv4.NewPacketConn(aControlConn)
	s.AControlPort = int(cport)
	go func() {
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("udp server start listen audio control port[%d]", s.AControlPort)
		defer logger.Printf("udp server stop listen audio control port[%d]", s.AControlPort)
		for !s.Stoped {
			if n, _, _, err := s.AControlConn.ReadFrom(bufUDP); err == nil {
				//logger.Printf("Package recv from AControlConn.len:%d\n", n)
				rtpBytes := make([]byte, n)
				s.AddInputBytes(n)
				copy(rtpBytes, bufUDP)
				pack := &RTPPack{
					Type:   RTP_TYPE_AUDIOCONTROL,
					Buffer: bytes.NewBuffer(rtpBytes),
				}
				s.HandleRTP(pack)
			} else {
				logger.Println("udp server read audio control pack error", err)
				continue
			}
		}
	}()
	return
}

func (s *UDPServer) SetupVideo() (err error) {
	server := GetServer()
	logger := s.Logger()
	vport, err := utils.FindAvailableUDPPort(server.rtpMinUdpPort, server.rtpMaxUdpPort)
	if err != nil {
		return
	}
	vConn, err := net.ListenPacket("udp4", fmt.Sprint(":", vport))
	if err != nil {
		logger.Printf("udp server video conn set listen error, %v", err)
		return
	}
	s.VConn = ipv4.NewPacketConn(vConn)
	s.VPort = int(vport)
	go func() {
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("udp server start listen video port[%d]", s.VPort)
		defer logger.Printf("udp server stop listen video port[%d]", s.VPort)
		timer := time.Unix(0, 0)
		for !s.Stoped {
			if n, _, _, err := s.VConn.ReadFrom(bufUDP); err == nil {
				elapsed := time.Now().Sub(timer)
				if elapsed >= 30*time.Second {
					logger.Printf("Package recv from VConn.len:%d\n", n)
					timer = time.Now()
				}
				rtpBytes := make([]byte, n)
				s.AddInputBytes(n)
				copy(rtpBytes, bufUDP)
				pack := &RTPPack{
					Type:   RTP_TYPE_VIDEO,
					Buffer: bytes.NewBuffer(rtpBytes),
				}
				s.HandleRTP(pack)
			} else {
				logger.Println("udp server read video pack error", err)
				continue
			}
		}
	}()

	cport, err := utils.FindAvailableUDPPort(server.rtpMinUdpPort, server.rtpMaxUdpPort)
	if err != nil {
		return
	}
	vControlConn, err := net.ListenPacket("udp4", fmt.Sprint(":", cport))
	if err != nil {
		logger.Printf("udp server video control conn set listen error, %v", err)
		return
	}
	s.VControlConn = ipv4.NewPacketConn(vControlConn)
	s.VControlPort = int(cport)
	go func() {
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("udp server start listen video control port[%d]", s.VControlPort)
		defer logger.Printf("udp server stop listen video control port[%d]", s.VControlPort)
		for !s.Stoped {
			if n, _, _, err := s.VControlConn.ReadFrom(bufUDP); err == nil {
				//logger.Printf("Package recv from VControlConn.len:%d\n", n)
				rtpBytes := make([]byte, n)
				s.AddInputBytes(n)
				copy(rtpBytes, bufUDP)
				pack := &RTPPack{
					Type:   RTP_TYPE_VIDEOCONTROL,
					Buffer: bytes.NewBuffer(rtpBytes),
				}
				s.HandleRTP(pack)
			} else {
				logger.Println("udp server read video control pack error", err)
				continue
			}
		}
	}()
	return
}
