package rtsp

import (
	"encoding/json"
	"fmt"
	"github.com/bruce-qin/EasyGoLib/utils"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"os"
	"time"

	"github.com/ReneKroon/ttlcache/v2"
)

type MulticastServer struct {
	SessionLogger

	connCache            *ttlcache.Cache
	pusherCache          *ttlcache.Cache
	conn                 *ipv4.PacketConn
	multiCmdAddr         *net.UDPAddr
	stopped              bool
	multicastCommandConn *net.UDPConn
	multicastRtpChan     chan *multicastRtpPack
	multicastCmdChan     chan *MulticastCommand
}

type multicastRtpPack struct {
	pack             *RTPPack
	multicastRtpAddr string
}

func InitializeMulticastServer() (mserver *MulticastServer, err error) {
	server := GetServer()
	mserver = &MulticastServer{
		stopped:          false,
		multicastRtpChan: make(chan *multicastRtpPack, 512),
		multicastCmdChan: make(chan *MulticastCommand, 512),
	}
	mserver.logger = log.New(os.Stdout, fmt.Sprintf("multicastRtspServer::[%s]", server.multicastAddr), log.LstdFlags|log.Lshortfile)
	if !server.enableMulticast {
		return
	}
	addr, err := net.ResolveUDPAddr("udp4", server.multicastAddr)
	if err != nil {
		return
	}
	conn, err := utils.ListenMulticastAddress(addr, server.multicastBindInf)
	if err != nil {
		return
	}
	mserver.multiCmdAddr = addr
	mserver.conn = conn
	mserver.connCache = ttlcache.NewCache()
	mserver.pusherCache = ttlcache.NewCache()
	udpConn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return
	}
	mserver.multicastCommandConn = udpConn
	mserver.pusherCache.SetExpirationCallback(func(path string, pusher interface{}) {
		storedPusher := server.GetPusher(path)
		if storedPusher != nil {
			storedPusher.Stop()
		}
	})
	mserver.connCache.SetExpirationCallback(func(key string, udpConn interface{}) {
		if conn := udpConn.(*net.UDPConn); conn != nil {
			if err := conn.Close(); err != nil {
				mserver.logger.Println("Close UDP Connection error", err)
			}
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
				var multiCommand MulticastCommand
				err := json.Unmarshal(multiInfoBuf, &multiCommand)
				if err != nil {
					logger.Printf("json Unmarshal error:%v", err)
					continue
				}
				//logger.Println("收到组播数据：", string(multiInfoBuf))
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
	go func() {
		for !mserver.stopped {
			select {
			case multicastRtpPack := <-mserver.multicastRtpChan:
				udpConn, err := mserver.getMulticastConnectionFromCache(multicastRtpPack.multicastRtpAddr)
				if err != nil {
					mserver.logger.Print("send rtppack error, address:", multicastRtpPack.multicastRtpAddr, err)
					return
				}
				_, err = udpConn.Write(multicastRtpPack.pack.Buffer.Bytes())
				if err != nil {
					mserver.logger.Print("send rtppack error, address:", multicastRtpPack.multicastRtpAddr, err)
				}
			case multiCommand := <-mserver.multicastCmdChan:
				bytes, err := json.Marshal(multiCommand)
				if err != nil {
					mserver.logger.Printf("json serialize error：%v \n", err)
				}
				_, err = mserver.multicastCommandConn.Write(bytes)
				if err != nil {
					mserver.logger.Println("multicast server send MulticastCommand pack error", err)
				}
			}
		}
	}()
	return
}

func (mserver *MulticastServer) SendMulticastCommandData(multiCommand *MulticastCommand) {
	mserver.multicastCmdChan <- multiCommand
}

func (mserver *MulticastServer) getMulticastConnectionFromCache(multicastAddress string) (udpConn *net.UDPConn, err error) {
	udpConnCache, cacheErr := mserver.connCache.Get(multicastAddress)
	if cacheErr != nil {
		var udpAddr *net.UDPAddr
		udpAddr, err = net.ResolveUDPAddr("udp4", multicastAddress)
		if err != nil {
			mserver.logger.Print("get multicast address cacheErr", err)
			return
		}
		udpConn, err = net.DialUDP("udp4", nil, udpAddr)
		mserver.connCache.SetWithTTL(multicastAddress, udpConn, time.Duration(30)*time.Second)
	} else {
		udpConn = udpConnCache.(*net.UDPConn)
	}
	return
}

func (mserver *MulticastServer) SendMulticastRtpPack(pack *RTPPack, multiInfo *MulticastCommunicateInfo) {
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
	mserver.multicastRtpChan <- &multicastRtpPack{
		pack:             pack,
		multicastRtpAddr: multicastAddress,
	}

}
