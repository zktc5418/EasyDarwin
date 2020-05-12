package rtsp

import (
	"encoding/json"
	"fmt"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"os"
	"time"

	"github.com/ReneKroon/ttlcache"
)

type MulticastServer struct {
	*SessionLogger

	cache        *ttlcache.Cache
	pusherCache  *ttlcache.Cache
	conn         *ipv4.PacketConn
	multiCmdAddr *net.UDPAddr
	stopped      bool
}

func InitializeMulticastServer() (mserver *MulticastServer, err error) {
	server := GetServer()
	mserver = &MulticastServer{
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
	conn, err := openMulticastConnection(addr, server.multicastBindInf)
	if err != nil {
		return
	}
	mserver.multiCmdAddr = addr
	mserver.conn = conn
	mserver.cache = ttlcache.NewCache()
	mserver.pusherCache = ttlcache.NewCache()
	mserver.pusherCache.SetExpirationCallback(func(path string, pusher interface{}) {
		storedPusher := server.GetPusher(path)
		if storedPusher != nil {
			storedPusher.Stop()
		}
	})
	go func() {
		logger := mserver.logger
		bufUDP := make([]byte, UDP_BUF_SIZE)
		logger.Printf("multicastServer start listen [%s]", server.multicastAddr)
		defer logger.Printf("multicastServer stop listen [%s]", server.multicastAddr)

		for !mserver.stopped {
			if n, _, _, err := mserver.conn.ReadFrom(bufUDP); err == nil {

				//elapsed := time.Now().Sub(timer)
				//if elapsed >= 30*time.Second {
				//    logger.Printf("Package recv from AConn.len:%d\n", n)
				//    timer = time.Now()
				//}
				multiInfoBuf := make([]byte, n)
				copy(multiInfoBuf, bufUDP)
				mserver.logger.Println("收到组播通信数据：", string(multiInfoBuf), ": 组播地址：", mserver.multiCmdAddr)
				var multiCommand MulticastCommand
				err := json.Unmarshal(multiInfoBuf, &multiCommand)
				if err != nil {
					logger.Printf("json Unmarshal error:%v", err)
					continue
				}
				if multiCommand.Command == START_MULTICAST {
					if pusher := server.GetPusher(multiCommand.MultiInfo.Path); pusher != nil {
						mserver.pusherCache.Get(multiCommand.MultiInfo.Path)
						continue
					}
					pusher := NewMulticastPusher(multiCommand.MultiInfo)
					//60秒未收到数据则停止pusher
					mserver.pusherCache.SetWithTTL(multiCommand.MultiInfo.Path, pusher, time.Duration(60)*time.Second)
					success := server.AddPusher(pusher)
					if success {
						logger.Printf("add multicast pusher success :%v", multiCommand.MultiInfo)
					} else {
						logger.Printf("add multicast pusher fail :%v", multiCommand.MultiInfo)
					}
				} else if multiCommand.Command == STOP_MULTICAST {
					if pusher := server.GetPusher(multiCommand.MultiInfo.Path); pusher != nil {
						pusher.Stop()
						mserver.pusherCache.Remove(multiCommand.MultiInfo.Path)
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
	mserver.logger.Println("发送组播通信数据：", string(bytes), ": 组播地址：", mserver.multiCmdAddr)
	_, err = conn.WriteTo(bytes, nil, mserver.multiCmdAddr)
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
	_, err := mserver.conn.WriteTo(pack.Buffer.Bytes(), nil, udpMultiAddr)
	if err != nil {
		mserver.logger.Print("send rtppack error, address:", udpMultiAddr, err)
	}
}

func openMulticastConnection(udpMultiAddr *net.UDPAddr, inf *net.Interface) (conn *ipv4.PacketConn, err error) {

	packet, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		log.Println(err)
		return
	}
	conn = ipv4.NewPacketConn(packet)
	err = conn.JoinGroup(inf, udpMultiAddr)
	if err != nil {
		log.Println(err)
		return
	}
	return
}
