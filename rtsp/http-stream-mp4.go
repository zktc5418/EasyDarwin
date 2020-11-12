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

const FFmpegMp4ConvertLocalCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -vn -nostdin -c:a aac -c:v libx264 -threads 8 -cpu-used 8 -quality realtime -qmax 51 -bufsize 11480k -movflags isml+frag_keyframe+negative_cts_offsets+omit_tfhd_offset -f mp4 -v error udp://127.0.0.1:%d"

const FFmpegMp4ConvertMulticastCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -vn -nostdin -c:a aac -c:v libx264 -threads 8 -cpu-used 8 -quality realtime -qmax 51 -bufsize 11480k -movflags isml+frag_keyframe+negative_cts_offsets+omit_tfhd_offset -f mp4 -v error udp://%s:%d?localaddr=%s"

var (
	Mp4StreamRouter *gin.Engine

	mp4StreamGinHandler *VideoStreamGinHandler
)

type VideoUdpDataListener struct {
	*MediaUdpDataListener
}

type VideoStreamGinHandler struct {
	*MediaStreamGinHandler
}

func InitMp4Stream() (err error) {
	Mp4StreamRouter = gin.New()
	mp4StreamGinHandler = &VideoStreamGinHandler{}
	pprof.Register(Mp4StreamRouter)
	Mp4StreamRouter.Use(gin.Recovery())
	Mp4StreamRouter.Use(Errors())
	Mp4StreamRouter.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET"},
		AllowCredentials: true,
		AllowAllOrigins:  true,
		MaxAge:           12 * time.Hour,
		AllowWildcard:    true,
	}))
	//id := shortid.MustGenerate()
	Mp4StreamRouter.Use(mp4StreamGinHandler.BeforeProcessMediaStream, mp4StreamGinHandler.ProcessMp4Stream)
	return nil
}

func NewMp4UdpDataListener(pusher *Pusher) (listener *VideoUdpDataListener) {
	return &VideoUdpDataListener{
		MediaUdpDataListener: newMediaStreamLocalListener(pusher),
	}
}

func (handler VideoStreamGinHandler) ProcessMp4Stream(c *gin.Context) {
	c.Status(200)
	c.Header("Content-Type", "video/mp4")
	streamInfo := generateHttpStreamInfo(c)
	server := GetServer()
	defer func() {
		streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
		streamInfo.overed = true
		delete(server.pushers[streamInfo.rtspPath].udpHttpAudioStreamListener.pullerMap, streamInfo.id)
	}()
	server.pushers[streamInfo.rtspPath].udpHttpAudioStreamListener.pullerMap[streamInfo.id] = streamInfo
	c.Stream(func(w io.Writer) bool {
		if !streamInfo.overed {
			data := <-streamInfo.mediaData
			if _, err := w.Write(*data); err == nil {
				return true
			} else {
				server.logger.Printf("write video data error:%v", err)
				return false
			}
		} else {
			return false
		}
	})
}

func (listener *VideoUdpDataListener) Start() (err error) {
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
			multicastInfo = pusher.RTSPClient.multicastInfo
		}
		if multicastInfo == nil {
			return fmt.Errorf("invalidation rtsp pusher, path:%s", listener.rtspPath)
		}
		//启用组播监听
		if conn, err = listener.doMediaStreamMulticastListen(multicastInfo.VideoStreamPort, multicastInfo.VideoMulticastAddress); err != nil {
			return err
		}
	} else {
		//发送到本地
		if conn, err = listener.doMediaStreamLocalListen(); err != nil {
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
				listener.logger.Printf("send [%s] mp4 udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
			}
		}()
		for !listener.closed {
			videoData := <-listener.mediaDataChan
			for key, puller := range listener.pullerMap {
				puller.mediaData <- videoData
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
				return err
			}
			if len(addrs) == 0 {
				return fmt.Errorf("invalidation mulicast interface:[%s]", GetServer().multicastBindInf.Name)
			}
			cmd = fmt.Sprintf(FFmpegMp4ConvertMulticastCmd, server.TCPPort, listener.rtspPath, multicastInfo.VideoMulticastAddress, multicastInfo.VideoStreamPort, addrs[0].String())
		}
	} else {
		//没有启用组播，发送到本地
		addr := conn.LocalAddr().(*net.UDPAddr)
		cmd = fmt.Sprintf(FFmpegMp4ConvertLocalCmd, server.TCPPort, listener.rtspPath, addr.Port)
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
		bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, server.logger, listener.rtspPath, listener.sessionId)
		listener.cmdBag = bag
		bag.Run(listener.Stop)
	}
	return nil
}

func (listener *VideoUdpDataListener) Stop() {
	defer func() {
		if stopErr := recover(); stopErr != nil {
			listener.logger.Printf("stop [%s] mp4 udp data error:%v", listener.rtspPath, stopErr)
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
