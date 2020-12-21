package rtsp

import (
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"golang.org/x/net/ipv4"
	"io"
	"net"
	"strings"
	"time"
)

const FFmpegMp3ConvertLocalCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -nostdin -vn -c:a libmp3lame -muxdelay 0.1 -flush_packets 1 -f mp3 -v error udp://127.0.0.1:%d"

const FFmpegMp3ConvertMulticastCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -nostdin -vn -c:a libmp3lame -muxdelay 0.1 -flush_packets 1 -f mp3 -v error udp://%s:%d?localaddr=%s"

var (
	Mp3StreamRouter *gin.Engine

	mp3StreamGinHandler *AudioStreamGinHandler
)

type AudioUdpDataListener struct {
	*MediaUdpDataListener
}

type AudioStreamGinHandler struct {
	*MediaStreamGinHandler
}

func InitMp3Stream() (err error) {
	Mp3StreamRouter = gin.New()
	mp3StreamGinHandler = &AudioStreamGinHandler{
		MediaStreamGinHandler: &MediaStreamGinHandler{},
	}
	pprof.Register(Mp3StreamRouter)
	Mp3StreamRouter.Use(gin.Recovery())
	Mp3StreamRouter.Use(Errors())
	Mp3StreamRouter.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET"},
		AllowCredentials: true,
		AllowAllOrigins:  true,
		MaxAge:           12 * time.Hour,
		AllowWildcard:    true,
	}))
	//id := shortid.MustGenerate()
	Mp3StreamRouter.Use(mp3StreamGinHandler.BeforeProcessMediaStream, mp3StreamGinHandler.ProcessMp3Stream)
	return nil
}

func NewMp3UdpDataListener(pusher *Pusher) (listener *AudioUdpDataListener) {
	return &AudioUdpDataListener{
		MediaUdpDataListener: newMediaStreamLocalListener(pusher),
	}
}

func (handler AudioStreamGinHandler) ProcessMp3Stream(c *gin.Context) {
	c.Status(200)
	c.Header("Content-Type", "audio/mpeg")
	streamInfo := generateHttpStreamInfo(c)
	server := GetServer()
	defer func() {
		streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
		streamInfo.overed = true
		delete(server.GetPusher(streamInfo.rtspPath).udpHttpAudioStreamListener.pullerMap, streamInfo.id)
	}()
	server.GetPusher(streamInfo.rtspPath).udpHttpAudioStreamListener.pullerMap[streamInfo.id] = streamInfo
	c.Stream(func(w io.Writer) bool {
		if !streamInfo.overed {
			data := <-streamInfo.mediaData
			if _, err := w.Write(*data); err == nil {
				return true
			} else {
				streamInfo.logger.Printf("write audio data error:%v", err)
				return false
			}
		} else {
			return false
		}
	})
}

func (listener *AudioUdpDataListener) Start() (err error) {
	server := GetServer()
	pusher := listener.pusher
	var (
		conn *ipv4.PacketConn
		//推流到当前服务器，需要执行ffmpeg命令
		currentPusher bool = false
		multicastInfo *MulticastCommunicateInfo
	)

	if server.enableMulticast {
		//使用组播通信
		if pusher.Session != nil {
			if multicastInfo = pusher.Session.multicastInfo; multicastInfo != nil {
				currentPusher = true
			}
		}
		if multicastInfo == nil && pusher.RTSPClient != nil {
			if multicastInfo = pusher.RTSPClient.multicastInfo; multicastInfo != nil {
				currentPusher = true
			}
		}
		if multicastInfo == nil && pusher.MulticastClient != nil {
			multicastInfo = pusher.MulticastClient.multiInfo
		}
		if multicastInfo == nil {
			return fmt.Errorf("invalidation rtsp pusher, path:%s", listener.rtspPath)
		}
		//启用组播监听
		listener.logger.Printf("http mp3 multicast listen multicast address:%s, port:%d", multicastInfo.AudioMulticastAddress, multicastInfo.AudioStreamPort)
		if conn, err = listener.doMediaStreamMulticastListen(multicastInfo.AudioStreamPort, multicastInfo.AudioMulticastAddress); err != nil {
			listener.logger.Printf("http mp3 multicast listen error:%v", err)
			return err
		}
	} else {
		//发送到本地
		if conn, err = listener.doMediaStreamLocalListen(); err != nil {
			listener.logger.Printf("http mp3 local listen error:%v", err)
			return err
		}
	}
	//向客户端写入数据
	go func() {
		defer func() {
			for _, puller := range listener.pullerMap {
				puller.overed = true
				close(puller.mediaData)
			}
		}()
		defer func() {
			if execErr := recover(); execErr != nil {
				listener.logger.Printf("send [%s] mp3 udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
			}
		}()
		for !listener.closed {
			audioData := <-listener.mediaDataChan
			for key, puller := range listener.pullerMap {
				select {
				case puller.mediaData <- audioData:
				default:
				}

				if puller.overed {
					delete(listener.pullerMap, key)
				}
			}
		}
	}()
	var cmd string
	if server.enableMulticast {
		//启用了组播集群，并且当前为推送源
		if currentPusher {
			addrs, err := GetServer().multicastBindInf.Addrs()
			if err != nil {
				listener.logger.Printf("get multicast interface bind ipv4 addr error:%v", err)
				return err
			}
			if len(addrs) == 0 {
				return fmt.Errorf("invalidation mulicast interface:[%s]", GetServer().multicastBindInf.Name)
			}
			if addr := getIpv4(addrs); addr != "" {
				cmd = fmt.Sprintf(FFmpegMp3ConvertMulticastCmd, server.TCPPort, listener.rtspPath, multicastInfo.AudioMulticastAddress, multicastInfo.AudioStreamPort, addr)
			} else {
				return fmt.Errorf("invalidation mulicast interface:[%s]", GetServer().multicastBindInf.Name)
			}
		}
	} else {
		//没有启用组播，发送到本地
		addr := conn.LocalAddr().(*net.UDPAddr)
		cmd = fmt.Sprintf(FFmpegMp3ConvertLocalCmd, server.TCPPort, listener.rtspPath, addr.Port)
	}
	if cmd != "" {
		cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
		parametersRaw := spaceRegex.Split(cmd, -1)
		var parameters []string
		for _, parameter := range parametersRaw {
			if parameter != "" {
				parameters = append(parameters, parameter)
			}
		}
		bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, pusher.Logger(), listener.rtspPath, listener.sessionId)
		listener.cmdBag = bag
		bag.Run(listener.Stop)
	}
	return nil
}

func (listener *AudioUdpDataListener) Stop() {
	defer func() {
		if stopErr := recover(); stopErr != nil {
			listener.logger.Printf("stop [%s] mp3 udp data error:%v", listener.rtspPath, stopErr)
		}
	}()
	if !listener.closed {
		listener.closed = true
		if listener.cmdBag != nil {
			listener.cmdBag.PushOver4Kill()
		}
		close(listener.mediaDataChan)
	}
}
