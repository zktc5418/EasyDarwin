package rtsp

import (
	"crypto/md5"
	"fmt"
	"github.com/ReneKroon/ttlcache"
	"github.com/bruce-qin/EasyGoLib/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/teris-io/shortid"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const GIN_HTTP_STREAM_INFO_STORAGE_KEY = "httpStreamInfo"

const cookieName string = "x-token"

var (
	tokenNonceCache *ttlcache.Cache
	spaceRegex      = regexp.MustCompile("[ ]+")
)

type MediaStreamGinHandler struct {
}

//http客户端拉流
type HttpPlayStreamInfo struct {
	id         string
	rtspPath   string
	fullPath   string
	nonce      string
	authCookie string
	overed     bool
	mediaData  chan *[]byte
	clientAdd  string
}

//rtsp服务器接收媒体流
type MediaUdpDataListener struct {
	cmdBag        *CmdRepeatBag
	rtspPath      string
	pullerMap     map[string]*HttpPlayStreamInfo
	closed        bool
	mediaDataChan chan *[]byte
	sessionId     string
	logger        *log.Logger
	pusher        *Pusher
}

func init() {
	server := GetServer()
	if (server.EnableAudioHttpStream || server.EnableVideoHttpStream) && (server.remoteHttpAuthorizationEnable || server.localAuthorizationEnable) {
		tokenNonceCache = ttlcache.NewCache()
	}
}

func (info *HttpPlayStreamInfo) ToRtspWebHookInfo(actionType WebHookActionType) (webHook *WebHookInfo) {
	return &WebHookInfo{
		ID:          info.id,
		ActionType:  actionType,
		TransType:   TRANS_TYPE_TCP.String(),
		SessionType: SESSEION_TYPE_PLAYER.String(),
		Path:        info.rtspPath,
		URL:         info.fullPath,
		SDP:         "",
		ClientAddr:  info.clientAdd,
	}
}

func newMediaStreamLocalListener(pusher *Pusher) *MediaUdpDataListener {
	return &MediaUdpDataListener{
		closed:        false,
		mediaDataChan: make(chan *[]byte, 128),
		rtspPath:      pusher.Path(),
		logger:        pusher.Logger(),
		pullerMap:     make(map[string]*HttpPlayStreamInfo),
		sessionId:     pusher.ID(),
		pusher:        pusher,
	}
}

func (listener *MediaUdpDataListener) doMediaStreamLocalListen() (*ipv4.PacketConn, error) {
	p, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	conn := ipv4.NewPacketConn(p)
	//监听udp音频数据
	go func() {
		defer conn.Close()
		defer func() {
			if execErr := recover(); execErr != nil {
				listener.logger.Printf("local listen [%s] media udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
			}
		}()
		buf := make([]byte, 2048)
		for !listener.closed {
			n, _, _, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				listener.logger.Printf("local receive media udp data path{%s} error:%v", listener.rtspPath, readErr)
				continue
			}
			mediaData := make([]byte, n) //buf[0:n]
			copy(mediaData, buf)
			listener.mediaDataChan <- &mediaData
		}
	}()
	return conn, nil
}

func (listener *MediaUdpDataListener) doMediaStreamMulticastListen(port uint16, multiAddr string) (*ipv4.PacketConn, error) {
	multiUdpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprint(multiAddr, ":", port))
	if err != nil {
		listener.logger.Printf("http stream multicast[%s:%d] address error, %v", multiAddr, port, err)
		return nil, err
	}
	conn, err := utils.ListenMulticastAddress(multiUdpAddr, GetServer().multicastBindInf)

	//conn, err = net.ListenMulticastUDP("udp", multiConn.Server.multicastBindInf, multiUdpAddr)
	if err != nil {
		listener.logger.Printf("http stream multicast[%s:%d] conn listen error, %v", multiAddr, port, err)
		return nil, err
	}
	//监听udp组播数据
	go func() {
		defer conn.Close()
		defer ReleaseMulticastAddress(multiAddr, port)
		defer func() {
			if execErr := recover(); execErr != nil {
				listener.logger.Printf("http stream multicast listen [%s] media udp data error:%v, addr:%s", listener.rtspPath, execErr, conn.LocalAddr())
			}
		}()
		AddExistMulticastAddress(multiAddr, port)
		buf := make([]byte, 2048)
		for !listener.closed {
			n, _, _, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				listener.logger.Printf("http stream multicast receive media udp data path{%s} error:%v", listener.rtspPath, readErr)
				continue
			}
			mediaData := make([]byte, n) //buf[0:n]
			copy(mediaData, buf)
			listener.mediaDataChan <- &mediaData
		}
	}()
	return conn, nil
}

func getIpv4(addrs []net.Addr) string {
	for _, addr := range addrs {
		s := addr.String()
		if strings.HasPrefix(s, "::1/") || strings.HasPrefix(s, "0:0:0:0:0:0:0:1/") || strings.Contains(s, "127.0.0.1") {
			continue
		}
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil && strings.Contains(ip.String(), ".") {
				return ip.String()
			}
		}
	}
	addr := GetAddr()
	GetServer().logger.Printf("could not find multicast ipv4 address:%v, return default: %s", addrs, addr)
	return addr
}

func GetAddr() string { //Get ip
	conn, err := net.Dial("udp", "baidu.com:80")
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	defer conn.Close()
	return strings.Split(conn.LocalAddr().String(), ":")[0]
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

func generateHttpStreamInfo(c *gin.Context) *HttpPlayStreamInfo {
	value, exists := c.Get(GIN_HTTP_STREAM_INFO_STORAGE_KEY)
	if exists {
		return value.(*HttpPlayStreamInfo)
	} else {
		id := shortid.MustGenerate()
		info := &HttpPlayStreamInfo{
			id:        id,
			rtspPath:  c.Request.URL.Path,
			fullPath:  c.Request.RequestURI,
			overed:    false,
			mediaData: make(chan *[]byte, 128),
			clientAdd: strings.Split(c.Request.RemoteAddr, ":")[0],
		}
		server := GetServer()
		if server.localAuthorizationEnable || server.remoteHttpAuthorizationEnable {
			var nonce, token string
			if token, _ = c.Cookie(cookieName); token != "" {
				if cacheNonce, exist := tokenNonceCache.Get(token); exist {
					nonce = cacheNonce.(string)
				}
			} else {
				token = fmt.Sprintf("%x", md5.Sum([]byte(shortid.MustGenerate())))
				c.SetCookie(cookieName, token, 60, "/", "", true, true)
			}
			if nonce == "" {
				nonce = fmt.Sprintf("%x", md5.Sum([]byte(shortid.MustGenerate())))
			}
			tokenNonceCache.SetWithTTL(token, nonce, time.Duration(60)*time.Second)
			info.nonce = nonce
		}
		c.Set(GIN_HTTP_STREAM_INFO_STORAGE_KEY, info)
		return info
	}
}

func (handler MediaStreamGinHandler) BeforeProcessMediaStream(c *gin.Context) {
	streamInfo := generateHttpStreamInfo(c)
	server := GetServer()
	logger := server.logger
	//身份认证
	if server.localAuthorizationEnable || server.remoteHttpAuthorizationEnable {
		authLine := c.GetHeader("Authorization")
		authFailed := true
		if authLine != "" {
			info, err := DecodeAuthorizationInfo(authLine, streamInfo.nonce, c.Request.Method, SESSEION_TYPE_PLAYER)
			if err != nil {
				logger.Printf("%v", err)
			} else if server.localAuthorizationEnable {
				if err = info.CheckAuthLocal(); err != nil {
					logger.Printf("%v", err)
				} else {
					authFailed = false
				}
			} else if server.remoteHttpAuthorizationEnable {
				if err = info.CheckAuthHttpRemote(); err != nil {
					logger.Printf("%v", err)
				} else {
					authFailed = false
				}
			}
		}
		if authFailed {
			if server.authorizationType == "Basic" {
				c.Header("WWW-Authenticate", `Basic realm="EasyDarwin"`)
			} else {
				c.Header("WWW-Authenticate", fmt.Sprintf(`Digest realm="EasyDarwin", nonce="%s", algorithm="MD5"`, streamInfo.nonce))
			}
			_ = c.AbortWithError(401, fmt.Errorf("Unauthorized"))
		}
	}
	//拉流通知
	webHookInfo := streamInfo.ToRtspWebHookInfo(ON_PLAY)
	if webHookInfo.ExecuteWebHookNotify() {
		if pusher := server.pushers[streamInfo.rtspPath]; pusher == nil {
			waitExist := false
			if server.streamNotExistHoldMillisecond != 0 {
				end := time.Now().Add(server.streamNotExistHoldMillisecond)
				for time.Now().Before(end) {
					pusher = server.pushers[streamInfo.rtspPath]
					if pusher == nil {
						time.Sleep(time.Duration(200) * time.Millisecond)
					} else {
						waitExist = true
						break
					}
				}
			}
			if !waitExist {
				streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
				fmt.Printf("not found stream:%s ,uri:%s \n", streamInfo.rtspPath, streamInfo.fullPath)
				_ = c.AbortWithError(404, fmt.Errorf("not found stream:%s", streamInfo.rtspPath))
			}
		}
	} else {
		streamInfo.ToRtspWebHookInfo(ON_STOP).ExecuteWebHookNotify()
		_ = c.AbortWithError(403, fmt.Errorf("server not allow pull stream:%s", streamInfo.rtspPath))
	}
}
