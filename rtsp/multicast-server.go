package rtsp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/ReneKroon/ttlcache"
)

type MulticastServer struct {
	*SessionLogger
	*Server

	cache   *ttlcache.Cache
	conn    *net.UDPConn
	stopped bool
}

func InitializeMulticastServer() (mserver *MulticastServer, err error) {
	server := GetServer()
	mserver = &MulticastServer{
		Server:  server,
		stopped: false,
		SessionLogger: &SessionLogger{
			logger: log.New(os.Stdout, fmt.Sprintf("multicastServer[%s]", server.multicastAddr), log.LstdFlags|log.Lshortfile),
		},
	}
	if !server.enableMulticast {
		return
	}
	addr, err := net.ResolveUDPAddr("udp", server.multicastAddr)
	if err != nil {
		return
	}
	conn, err := net.ListenMulticastUDP("udp", server.multicastBindInf, addr)
	if err != nil {
		return
	}
	mserver.conn = conn
	mserver.cache = ttlcache.NewCache()
	go func() {
		logger := mserver.logger
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("multicastServer start listen [%s]", server.multicastAddr)
		defer logger.Printf("multicastServer stop listen [%s]", server.multicastAddr)
		for !mserver.stopped {
			if n, _, err := mserver.conn.ReadFromUDP(bufUDP); err == nil {

				//elapsed := time.Now().Sub(timer)
				//if elapsed >= 30*time.Second {
				//    logger.Printf("Package recv from AConn.len:%d\n", n)
				//    timer = time.Now()
				//}
				multiInfoBuf := make([]byte, n)
				copy(multiInfoBuf, bufUDP)
				var multiCommand MulticastCommand
				err := json.Unmarshal(multiInfoBuf, &multiCommand)
				if err != nil {
					logger.Printf("json Unmarshal error:%v", err)
					continue
				}
				if multiCommand.Command == START_MULTICAST {
					if pusher := server.GetPusher(multiCommand.MultiInfo.Path); pusher != nil {
						continue
					}
					pusher := NewMulticastPusher(multiCommand.MultiInfo)
					success := server.AddPusher(pusher)
					if success {
						logger.Printf("add multicast pusher success :%v", multiCommand.MultiInfo)
					} else {
						logger.Printf("add multicast pusher fail :%v", multiCommand.MultiInfo)
					}
				} else if multiCommand.Command == STOP_MULTICAST {
					if pusher := server.GetPusher(multiCommand.MultiInfo.Path); pusher != nil {
						pusher.Stop()
					}
				}
			} else {
				logger.Println("multicast server read MulticastCommand pack error", err)
				continue
			}
		}
	}()
	return
}

func (mserver *MulticastServer) SendMulticastCommandData(multiCommand *MulticastCommand) {
	//if !mserver.Server.enableMulticast {
	//    return
	//}
	bytes, err := json.Marshal(multiCommand)
	if err != nil {
		mserver.logger.Printf("json serialize error：%v \n", err)
	}
	conn := mserver.conn
	_, err = conn.Write(bytes)
	if err != nil {
		mserver.logger.Println("multicast server send MulticastCommand pack error", err)
	}
}

func (mserver *MulticastServer) SendMulticastRtpPack(pack *RTPPack, multiInfo *MulticastCommunicateInfo) {
	//if !mserver.Server.enableMulticast {
	//    return
	//}
	var multicastAddress string
	switch pack.Type {
	case RTP_TYPE_AUDIO:
		multicastAddress = fmt.Sprint(multiInfo.AudioRtpMultiAddress, ":", multiInfo.AudioRtpPort)
	case RTP_TYPE_AUDIOCONTROL:
		multicastAddress = fmt.Sprint(multiInfo.CtlAudioRtpMultiAddress, ":", multiInfo.CtlAudioRtpPort)
	case RTP_TYPE_VIDEO:
		multicastAddress = fmt.Sprint(multiInfo.VideoRtpMultiAddress, ":", multiInfo.VideoRtpPort)
	case RTP_TYPE_VIDEOCONTROL:
		multicastAddress = fmt.Sprint(multiInfo.CtlVideoRtpMultiAddress, ":", multiInfo.CtlVideoRtpPort)
	}
	var udpMultiAddr *net.UDPAddr
	udpAddr, success := mserver.cache.Get(multicastAddress)
	if !success {
		udpAddr, err := net.ResolveUDPAddr("udp", multicastAddress)
		if err != nil {
			mserver.logger.Print("send rtppack error, multicast address error", err)
			return
		}
		mserver.cache.SetWithTTL(multicastAddress, udpAddr, time.Duration(30)*time.Second)
		udpMultiAddr = udpAddr
	} else {
		udpMultiAddr = udpAddr.(*net.UDPAddr)
	}
	_, err := mserver.conn.WriteToUDP(pack.Buffer.Bytes(), udpMultiAddr)
	if err != nil {
		mserver.logger.Print("send rtppack error, address:", udpMultiAddr, err)
	}
}
