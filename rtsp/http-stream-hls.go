package rtsp

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"time"
)

//const (
//    FFmpegMp4ConvertHeaderCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -nostdin -y -c:a aac -c:v libx264 -threads 4 -cpu-used 4 -quality realtime -t 2 -v error -movflags faststart -bufsize 4k -f mp4 %s"
//
//    FFmpegMp4ConvertLocalCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -nostdin -c:a aac -c:v libx264 -threads 4 -cpu-used 4 -quality realtime -qmax 51 -bufsize 480k -flush_packets 0 -fflags +flush_packets+sortdts+genpts -movflags isml+frag_keyframe+negative_cts_offsets+omit_tfhd_offset+empty_moov -f mp4 -v error udp://127.0.0.1:%d"
//
//    FFmpegMp4ConvertMulticastCmd string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -nostdin -c:a aac -c:v libx264 -threads 4 -cpu-used 4 -quality realtime -qmax 51 -bufsize 480k -flush_packets 0 -fflags +flush_packets+sortdts+genpts -movflags isml+frag_keyframe+negative_cts_offsets+omit_tfhd_offset+empty_moov -f mp4 -v error udp://%s:%d?localaddr=%s"
//)

var (
	HlsStreamRouter *gin.Engine

	hlsStreamGinHandler *VideoStreamGinHandler
)

type VideoUdpDataListener struct {
	*MediaUdpDataListener
	decodeMp4Error bool
}

type VideoStreamGinHandler struct {
	*MediaStreamGinHandler
}

func InitHlsStream() (err error) {
	HlsStreamRouter = gin.New()
	hlsStreamGinHandler = &VideoStreamGinHandler{
		MediaStreamGinHandler: &MediaStreamGinHandler{},
	}
	pprof.Register(HlsStreamRouter)
	HlsStreamRouter.Use(gin.Recovery())
	HlsStreamRouter.Use(Errors())
	HlsStreamRouter.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET"},
		AllowCredentials: true,
		AllowAllOrigins:  true,
		MaxAge:           12 * time.Hour,
		AllowWildcard:    true,
	}))
	//id := shortid.MustGenerate()
	HlsStreamRouter.Use(hlsStreamGinHandler.BeforeProcessMediaStream)
	HlsStreamRouter.Static("/", GetServer().NginxRtmpHlsMapDir)
	return nil
}

//func NewMp4UdpDataListener(pusher *Pusher) (listener *VideoUdpDataListener) {
//    return &VideoUdpDataListener{
//        MediaUdpDataListener: newMediaStreamLocalListener(pusher),
//        decodeMp4Error: true,
//    }
//}

//func (handler VideoStreamGinHandler) ProcessMp4Stream(c *gin.Context) {
//    c.Status(200)
//    c.Header("Content-Type", "video/mp4")
//    streamInfo := generateHttpStreamInfo(c)
//    server := GetServer()
//    defer func() {
//        streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
//        streamInfo.overed = true
//        delete(server.pushers[streamInfo.rtspPath].udpHttpVideoStreamListener.pullerMap, streamInfo.id)
//    }()
//    listener := server.pushers[streamInfo.rtspPath].udpHttpVideoStreamListener
//    if listener.mp4Header == nil {
//        endTime := time.Now().Add(time.Duration(3) * time.Second)
//        for listener.mp4Header == nil {
//            time.Sleep(time.Duration(300) * time.Millisecond)
//            if time.Now().After(endTime) {
//                c.AbortWithStatusJSON(500, "mp4 header not found")
//            }
//        }
//    }
//    _ = listener.mp4Header.Encode(c.Writer)
//    listener.pullerMap[streamInfo.id] = streamInfo
//    c.Stream(func(w io.Writer) bool {
//        if !streamInfo.overed {
//            data := <-streamInfo.mediaData
//            if _, err := w.Write(*data); err == nil {
//                return true
//            } else {
//                server.logger.Printf("write video data error:%v", err)
//                return false
//            }
//        } else {
//            return false
//        }
//    })
//}

//func (listener *VideoUdpDataListener) Start() (err error) {
//    server := GetServer()
//    pusher := listener.pusher
//    var (
//        conn *ipv4.PacketConn
//        //推流到当前服务器，需要执行ffmpeg命令
//        currentPusher = false
//        multicastInfo *MulticastCommunicateInfo
//    )
//
//    if server.enableMulticast {
//        //使用组播通信
//        if pusher.Session != nil {
//            if multicastInfo = pusher.Session.multicastInfo; multicastInfo != nil {
//                currentPusher = true
//            }
//        }
//        if multicastInfo == nil && pusher.RTSPClient != nil {
//            if multicastInfo = pusher.RTSPClient.multicastInfo; multicastInfo != nil {
//                currentPusher = true
//            }
//        }
//        if multicastInfo == nil && pusher.MulticastClient != nil {
//            multicastInfo = pusher.MulticastClient.multiInfo
//        }
//        if multicastInfo == nil {
//            return fmt.Errorf("invalidation rtsp pusher, path:%s", listener.rtspPath)
//        }
//        //启用组播监听
//        if conn, err = listener.doMediaStreamMulticastListen(multicastInfo.VideoStreamPort, multicastInfo.VideoMulticastAddress); err != nil {
//            return err
//        }
//    } else {
//        //发送到本地
//        if conn, err = listener.doMediaStreamLocalListen(); err != nil {
//            return err
//        }
//    }
//    //向客户端写入数据
//    go func() {
//        defer func() {
//            for _, puller := range listener.pullerMap {
//                puller.overed = true
//                close(puller.mediaData)
//            }
//        }()
//        defer func() {
//            if execErr := recover(); execErr != nil {
//                listener.logger.Printf("send [%s] mp4 udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
//            }
//        }()
//        for !listener.closed {
//            videoData := <-listener.mediaDataChan
//            for key, puller := range listener.pullerMap {
//                puller.mediaData <- videoData
//                if puller.overed {
//                    delete(listener.pullerMap, key)
//                }
//            }
//        }
//    }()
//    var cmd string
//    if server.enableMulticast {
//        //启用了组播集群，并且当前为推送源
//        if currentPusher {
//            addrs, err := GetServer().multicastBindInf.Addrs()
//            if err != nil {
//                return err
//            }
//            if len(addrs) == 0 {
//                return fmt.Errorf("invalidation mulicast interface:[%s]", GetServer().multicastBindInf.Name)
//            }
//            if addr := getIpv4(addrs); addr != "" {
//                cmd = fmt.Sprintf(FFmpegMp4ConvertMulticastCmd, server.TCPPort, listener.rtspPath, multicastInfo.VideoMulticastAddress, multicastInfo.VideoStreamPort, addr)
//            } else {
//                return fmt.Errorf("invalidation mulicast interface:[%s]", GetServer().multicastBindInf.Name)
//            }
//        }
//    } else {
//        //没有启用组播，发送到本地
//        addr := conn.LocalAddr().(*net.UDPAddr)
//        cmd = fmt.Sprintf(FFmpegMp4ConvertLocalCmd, server.TCPPort, listener.rtspPath, addr.Port)
//    }
//    if cmd != "" {
//        cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
//        parametersRaw := spaceRegex.Split(cmd, -1)
//        var parameters []string
//        for _, parameter := range parametersRaw {
//            if parameter != "" {
//                parameters = append(parameters, parameter)
//            }
//        }
//        bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, server.logger, listener.rtspPath, listener.sessionId)
//        listener.cmdBag = bag
//        bag.Run(listener.Stop)
//    }
//    go func() {
//        mp4FileName := listener.rtspPath[strings.LastIndex(listener.rtspPath, "/")+1:]
//        fullPath := filepath.Join(tmpDirPath, mp4FileName + ".mp4")
//        cmd = fmt.Sprintf(FFmpegMp4ConvertHeaderCmd, server.TCPPort, listener.rtspPath, fullPath)
//        cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
//        parametersRaw := spaceRegex.Split(cmd, -1)
//        var parameters []string
//        for _, parameter := range parametersRaw {
//            if parameter != "" {
//                parameters = append(parameters, parameter)
//            }
//        }
//        bag := NewCmdRepeatBag(server.ffmpeg, parameters, 0, server.logger, listener.rtspPath, listener.sessionId)
//        bag.Run(func() {
//            if mp4File, err := os.OpenFile(fullPath, os.O_RDWR, 0644); err != nil{
//                listener.logger.Printf("rtsp:%s read mp4 tmp header file error:%v", listener.rtspPath, err)
//            }else if mp4Header, err := mp4.Decode(mp4File); err != nil{
//                listener.logger.Printf("rtsp:%s decode mp4 header file error:%v", listener.rtspPath, err)
//            }else {
//                marshal, _ := json.Marshal(mp4Header)
//                listener.logger.Printf("rtsp:%s decode mp4 header file result:%v", fullPath, string(marshal))
//                listener.mp4Header = mp4Header
//                mp4Header.Moov.Mvhd.Duration = 0
//                listener.decodeMp4Error = false
//            }
//            _ = os.Remove(fullPath)
//        })
//    }()
//    return nil
//}
//
//func (listener *VideoUdpDataListener) Stop() {
//    defer func() {
//        if stopErr := recover(); stopErr != nil {
//            listener.logger.Printf("stop [%s] mp4 udp data error:%v", listener.rtspPath, stopErr)
//        }
//    }()
//    if !listener.closed {
//        listener.closed = true
//        if listener.cmdBag != nil {
//            listener.cmdBag.PushOver4Kill()
//        }
//        close(listener.mediaDataChan)
//    }
//}
