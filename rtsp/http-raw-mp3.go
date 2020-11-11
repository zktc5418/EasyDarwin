package rtsp

import (
	"fmt"
	"github.com/bruce-qin/EasyGoLib/utils"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/teris-io/shortid"
	"golang.org/x/net/ipv4"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const FFMPEG_MP3_CONVERT_CMD string = "ffmpeg -rtsp_transport tcp -i rtsp://127.0.0.1:%d%s -vn -c:a libmp3lame -f mp3 -v error udp://127.0.0.1:%d"

const GIN_MP3_INFO_STORAGE_KEY = "mp3StreamInfo"

type Mp3StreamGinHandler struct {
}

type Mp3PlayStreamInfo struct {
	id        string
	rtspPath  string
	fullPath  string
	overed    bool
	mediaData chan *[]byte
}

type Mp3UdpDataListener struct {
	cmdBag        *CmdRepeatBag
	udpListenPort uint16
	rtspPath      string
	pullerMap     map[string]*Mp3PlayStreamInfo
	closed        bool
	audioDataChan chan *[]byte
	sessionId     string
	logger        *log.Logger
}

var spaceRegex = regexp.MustCompile("[ ]+")

var Mp3StreamRouter *gin.Engine

var mp3StreamGinHandler *Mp3StreamGinHandler

func (info *Mp3PlayStreamInfo) ToRtspWebHookInfo(actionType WebHookActionType) (webHook *WebHookInfo) {
	return &WebHookInfo{
		ID:          info.id,
		ActionType:  actionType,
		TransType:   TRANS_TYPE_TCP.String(),
		SessionType: SESSEION_TYPE_PLAYER.String(),
		Path:        info.rtspPath,
		URL:         info.fullPath,
		SDP:         "",
	}
}

func InitMp3Stream() (err error) {
	Mp3StreamRouter = gin.New()
	mp3StreamGinHandler = &Mp3StreamGinHandler{}
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
	Mp3StreamRouter.Use(mp3StreamGinHandler.BeforeProcessMp3Stream, mp3StreamGinHandler.ProcessMp3Stream)
	return nil
}

func Errors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, err := range c.Errors {
			switch err.Type {
			case gin.ErrorTypeBind:
				switch err.Err.(type) {
				case validator.ValidationErrors:
					errs := err.Err.(validator.ValidationErrors)
					for _, err := range errs {
						sec := utils.Conf().Section("localize")
						field := sec.Key(err.Field()).MustString(err.Field())
						tag := sec.Key(err.Tag()).MustString(err.Tag())
						c.AbortWithStatusJSON(http.StatusBadRequest, fmt.Sprintf("%s %s", field, tag))
						return
					}
				default:
					log.Println(err.Err.Error())
					c.AbortWithStatusJSON(http.StatusBadRequest, "Inner Error")
					return
				}
			}
		}
	}
}

func generateMp3StreamInfo(c *gin.Context) *Mp3PlayStreamInfo {
	value, exists := c.Get(GIN_MP3_INFO_STORAGE_KEY)
	if exists {
		return value.(*Mp3PlayStreamInfo)
	} else {
		info := &Mp3PlayStreamInfo{
			id:        shortid.MustGenerate(),
			rtspPath:  c.Request.URL.Path,
			fullPath:  c.Request.RequestURI,
			overed:    false,
			mediaData: make(chan *[]byte, 128),
		}
		c.Set(GIN_MP3_INFO_STORAGE_KEY, info)
		return info
	}
}

func (handler Mp3StreamGinHandler) BeforeProcessMp3Stream(c *gin.Context) {
	streamInfo := generateMp3StreamInfo(c)
	webHookInfo := streamInfo.ToRtspWebHookInfo(ON_PLAY)
	if webHookInfo.ExecuteWebHookNotify() {
		if pusher := GetServer().pushers[streamInfo.rtspPath]; pusher == nil {
			streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
			fmt.Printf("not found stream:%s ,uri:%s \n", streamInfo.rtspPath, streamInfo.fullPath)
			_ = c.AbortWithError(404, fmt.Errorf("not found stream:%s", streamInfo.rtspPath))
		}
	} else {
		streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
		_ = c.AbortWithError(403, fmt.Errorf("server not allow pull stream:%s", streamInfo.rtspPath))
	}
}

func (handler Mp3StreamGinHandler) ProcessMp3Stream(c *gin.Context) {
	c.Status(200)
	c.Header("Content-Type", "audio/mpeg")
	streamInfo := generateMp3StreamInfo(c)
	server := GetServer()
	defer func() {
		streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
		streamInfo.overed = true
		delete(server.pushers[streamInfo.rtspPath].udpHttpListener.pullerMap, streamInfo.id)
	}()
	server.pushers[streamInfo.rtspPath].udpHttpListener.pullerMap[streamInfo.id] = streamInfo
	c.Stream(func(w io.Writer) bool {
		if !streamInfo.overed {
			data := <-streamInfo.mediaData
			if _, err := w.Write(*data); err == nil {
				return true
			} else {
				server.logger.Printf("write audio data error:%v", err)
				return false
			}
		} else {
			return false
		}
	})
}

func NewMp3UdpDataListener(path, sessionId string, logger *log.Logger) (listener *Mp3UdpDataListener) {
	return &Mp3UdpDataListener{
		closed:        false,
		audioDataChan: make(chan *[]byte, 10),
		rtspPath:      path,
		logger:        logger,
		pullerMap:     make(map[string]*Mp3PlayStreamInfo),
		sessionId:     sessionId,
	}
}

func (listener *Mp3UdpDataListener) Start() (err error) {
	p, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return
	}
	p.LocalAddr()
	conn := ipv4.NewPacketConn(p)
	//监听udp音频数据
	go func() {
		defer conn.Close()
		defer func() {
			if execErr := recover(); execErr != nil {
				listener.logger.Printf("listen [%s] mp3 udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
			}
		}()
		buf := make([]byte, 2048)
		for !listener.closed {
			n, _, _, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				listener.logger.Printf("receive mp3 udp data path{%s} error:%v", listener.rtspPath, readErr)
				continue
			}
			audioData := buf[0:n]
			listener.audioDataChan <- &audioData
		}
	}()
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
			audioData := <-listener.audioDataChan
			for key, puller := range listener.pullerMap {
				puller.mediaData <- audioData
				if puller.overed {
					delete(listener.pullerMap, key)
				}
			}
		}
	}()
	server := GetServer()
	addr := conn.LocalAddr().(*net.UDPAddr)
	cmd := fmt.Sprintf(FFMPEG_MP3_CONVERT_CMD, server.TCPPort, listener.rtspPath, addr.Port)
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
	listener.udpListenPort = uint16(addr.Port)
	bag.Run(listener.Stop)
	return nil
}

func (listener *Mp3UdpDataListener) Stop() {
	defer func() {
		if stopErr := recover(); stopErr != nil {
			listener.logger.Printf("stop [%s] mp3 udp data error:%v", listener.rtspPath, stopErr)
		}
	}()
	if !listener.closed {
		listener.closed = true
		listener.cmdBag.PushOver4Kill()
		close(listener.audioDataChan)
	}
}
