package rtsp

import (
	"fmt"
	"github.com/emirpasic/gods/sets/hashset"
	"log"
	"net"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruce-qin/EasyGoLib/utils"
)

type Server struct {
	SessionLogger
	TCPListener       *net.TCPListener
	TCPPort           int
	Stoped            bool
	pushers           *sync.Map //map[string]*Pusher // Path <-> Pusher
	pushersCache      map[string]*Pusher
	pushersCacheQueue chan bool
	//pushersLock                   sync.RWMutex
	addPusherCh                   chan *Pusher
	removePusherCh                chan *Pusher
	rtpMinUdpPort                 uint16
	rtpMaxUdpPort                 uint16
	networkBuffer                 int
	localRecord                   byte
	ffmpeg                        string
	m3u8DirPath                   string
	tsDurationSecond              int
	gopCacheEnable                bool
	debugLogEnable                bool
	playerQueueLimit              int
	dropPacketWhenPaused          bool
	rtspWriteTimeoutMillisecond   int
	rtspReadTimeoutMillisecond    int
	streamNotExistHoldMillisecond time.Duration
	localAuthorizationEnable      bool
	remoteHttpAuthorizationEnable bool
	remoteHttpAuthorizationUrl    string
	authorizationType             AuthorizationType
	EnableAudioHttpStream         bool
	HttpAudioStreamPort           uint16
	EnableVideoHttpStream         bool
	HttpVideoStreamPort           uint16
	NginxRtmpHlsMapDir            string
	closeOld                      bool
	//svcDiscoverMultiAddr          string
	//svcDiscoverMultiPort          uint16
	enableMulticast  bool
	multicastAddr    string
	multicastBindInf *net.Interface
	mserver          *MulticastServer
	// /live1/stream123   key::live1 执行命令map
	// /live2/stream123	  key::live2 执行命令map
	// 环境变量：EASYDARWIN_PUSH_FFMPEG_MAP_CMD_key=
	pushCmdDirMap map[string][]string
	// /asd/streamsad	  key没有在map中执行的命令
	// EASYDARWIN_PUSH_FFMPEG_OTHER_CMD
	otherPushCmd []string
	// 所有命令推流都要执行的命令
	// 环境变量：EASYDARWIN_PUSH_FFMPEG_CMD
	allPushCmd []string
	// 命令执行错误时重试次数
	// 环境变量：EASYDARWIN_CMD_ERROR_REPEAT_TIME
	cmdErrorRepeatTime uint8
	//开始拉流时触发api调用
	// 环境变量：EASYDARWIN_REST_API_ON_PLAY
	onPlay []string
	//停止拉流时触发api调用
	// 环境变量：EASYDARWIN_REST_API_ON_STOP
	onStop []string
	//开始推流时触发api调用
	// 环境变量：EASYDARWIN_REST_API_ON_PUBLISH
	onPublish []string
	//停止推流时触发api调用
	// 环境变量：EASYDARWIN_REST_API_ON_TEARDOWN
	onTeardown []string
}

var Instance *Server = func() (server *Server) {
	logger := SessionLogger{log.New(os.Stdout, "[RTSPServer]", log.LstdFlags|log.Lshortfile)}
	rtspFile := utils.Conf().Section("rtsp")
	rtpPortRange := rtspFile.Key("rtpserver_udport_range").MustString("10000:60000")
	rtpMinPort, err := strconv.ParseUint(rtpPortRange[:strings.Index(rtpPortRange, ":")], 10, 16)
	if err != nil {
		logger.logger.Printf("invalidate rtp udp port: %v", err)
		rtpMinPort = 10000
	}
	rtpMaxPort, err := strconv.ParseUint(rtpPortRange[strings.Index(rtpPortRange, ":")+1:], 10, 16)
	if err != nil {
		logger.logger.Printf("invalidate rtp udp port: %v", err)
		rtpMaxPort = 60000
	}
	networkBuffer := rtspFile.Key("network_buffer").MustInt(1048576)
	localRecord := rtspFile.Key("save_stream_to_local").MustUint(0)
	ffmpeg := rtspFile.Key("ffmpeg_path").MustString("ffmpeg")
	m3u8_dir_path := rtspFile.Key("m3u8_dir_path").MustString("")
	ts_duration_second := rtspFile.Key("ts_duration_second").MustInt(6)
	infName := rtspFile.Key("multicast_svc_bind_inf").MustString("")
	var multicastBindInf *net.Interface = nil
	if infName != "" {
		multicastBindInf, err = net.InterfaceByName(infName)
		if err != nil {
			logger.logger.Fatalf("multicast interfaces {%v} not found", err)
		}
	} else {
		multicastBindInf = utils.FindSupportMulticastInterface()
		if multicastBindInf == nil {
			logger.logger.Fatalf("no multicast interfaces found")
		}
	}
	var (
		onPlay     []string
		onStop     []string
		onPublish  []string
		onTeardown []string
	)
	if httpApis := rtspFile.Key("on_play").Value(); httpApis != "" {
		onPlay = strings.Split(httpApis, ";")
	}
	if httpApis := rtspFile.Key("on_stop").Value(); httpApis != "" {
		onStop = strings.Split(httpApis, ";")
	}
	if httpApis := rtspFile.Key("on_publish").Value(); httpApis != "" {
		onPublish = strings.Split(httpApis, ";")
	}
	if httpApis := rtspFile.Key("on_teardown").Value(); httpApis != "" {
		onTeardown = strings.Split(httpApis, ";")
	}

	var (
		allCmds          []string
		otherCmds        []string
		envRepeatTime    uint8
		pushCmdMap       = make(map[string][]string)
		environs         = os.Environ()
		envKey           = "EASYDARWIN_PUSH_FFMPEG_CMD="
		repeatTimeEnvKey = "EASYDARWIN_CMD_ERROR_REPEAT_TIME="
		otherCMDEnvKey   = "EASYDARWIN_PUSH_FFMPEG_OTHER_CMD="
		mapCMDEnvKey     = "EASYDARWIN_PUSH_FFMPEG_MAP_CMD_"
		equalRegx        = regexp.MustCompile("=")
	)
	for _, environ := range environs {
		if strings.HasPrefix(environ, envKey) {
			envVal := environ[len(envKey):]
			allCmds = append(allCmds, strings.Split(envVal, ";")...)
		} else if strings.HasPrefix(environ, repeatTimeEnvKey) {
			envVal := environ[len(repeatTimeEnvKey):]
			if intRaw, err := strconv.ParseUint(envVal, 0, 8); err == nil {
				envRepeatTime = uint8(intRaw)
			}
		} else if strings.HasPrefix(environ, otherCMDEnvKey) {
			envVal := environ[len(otherCMDEnvKey):]
			otherCmds = append(otherCmds, strings.Split(envVal, ";")...)
		} else if strings.HasPrefix(environ, mapCMDEnvKey) {
			split := equalRegx.Split(environ, 2)
			pathKey := split[0][len(mapCMDEnvKey):]
			pushCmdMap[pathKey] = append(pushCmdMap[pathKey], strings.Split(split[1], ";")...)
		}
	}
	var (
		emptyAllCmds   = len(allCmds) == 0
		emptyOtherCmds = len(otherCmds) == 0
		emptyMapCmds   = len(pushCmdMap) == 0
		cmdKeys        = utils.Conf().Section("cmd").Keys()
	)
	for _, key := range cmdKeys {
		if emptyAllCmds && strings.HasPrefix(key.Name(), "all_execute_") {
			allCmds = append(allCmds, key.Value())
		}
		if emptyOtherCmds && strings.HasPrefix(key.Name(), "other_execute_") {
			otherCmds = append(otherCmds, key.Value())
		}
		if emptyMapCmds && strings.HasPrefix(key.Name(), "map_execute_") {
			dirKey := strings.Replace(key.Name(), "map_execute_", "", 1)
			pushers := pushCmdMap[dirKey]
			pushCmdMap[dirKey] = append(pushers, key.Value())
		}
	}
	if len(allCmds) > 0 {
		logger.logger.Printf("pusher cmds: \n %s", strings.Join(allCmds, "\n"))
	}
	if envRepeatTime == 0 {
		envRepeatTime = uint8(utils.Conf().Section("cmd").Key("cmd_error_repeat_time").MustUint(5))
	}
	server = &Server{
		SessionLogger:                 logger,
		Stoped:                        true,
		TCPPort:                       rtspFile.Key("port").MustInt(554),
		pushers:                       &sync.Map{},
		pushersCache:                  make(map[string]*Pusher),
		pushersCacheQueue:             make(chan bool, 1),
		addPusherCh:                   make(chan *Pusher),
		removePusherCh:                make(chan *Pusher),
		rtpMinUdpPort:                 uint16(rtpMinPort),
		rtpMaxUdpPort:                 uint16(rtpMaxPort),
		networkBuffer:                 networkBuffer,
		localRecord:                   byte(localRecord),
		ffmpeg:                        ffmpeg,
		m3u8DirPath:                   m3u8_dir_path,
		tsDurationSecond:              ts_duration_second,
		gopCacheEnable:                rtspFile.Key("gop_cache_enable").MustBool(true),
		debugLogEnable:                rtspFile.Key("debug_log_enable").MustBool(false),
		playerQueueLimit:              rtspFile.Key("player_queue_limit").MustInt(0),
		dropPacketWhenPaused:          rtspFile.Key("drop_packet_when_paused").MustBool(false),
		rtspWriteTimeoutMillisecond:   rtspFile.Key("write_timeout").MustInt(20000),
		rtspReadTimeoutMillisecond:    rtspFile.Key("read_timeout").MustInt(40000),
		streamNotExistHoldMillisecond: time.Duration(rtspFile.Key("stream_notexist_wait_second").MustInt(10)) * time.Second,
		localAuthorizationEnable:      rtspFile.Key("local_authorization_enable").MustBool(false),
		remoteHttpAuthorizationEnable: rtspFile.Key("remote_http_authorization_enable").MustBool(false),
		remoteHttpAuthorizationUrl:    rtspFile.Key("remote_http_authorization_url").Value(),
		authorizationType:             AuthorizationType(rtspFile.Key("authorization_type").Value()),
		closeOld:                      rtspFile.Key("close_old").MustBool(false),
		//svcDiscoverMultiAddr:          rtspFile.Key("svc_discover_multiaddr").MustString("239.12.12.12"),
		//svcDiscoverMultiPort:          uint16(rtspFile.Key("svc_discover_multiport").MustUint(1212)),
		enableMulticast:       rtspFile.Key("enable_multicast").MustBool(true),
		multicastAddr:         rtspFile.Key("multicast_svc_discover_addr").MustString("232.2.2.2:8760"),
		multicastBindInf:      multicastBindInf,
		EnableAudioHttpStream: rtspFile.Key("enable_http_audio_stream").MustBool(true),
		HttpAudioStreamPort:   uint16(rtspFile.Key("http_audio_stream_port").MustUint(8088)),
		EnableVideoHttpStream: rtspFile.Key("enable_http_video_stream").MustBool(false),
		HttpVideoStreamPort:   uint16(rtspFile.Key("http_video_stream_port").MustUint(8099)),
		NginxRtmpHlsMapDir:    rtspFile.Key("nginx_rtmp_hls_dir_map").MustString("record"),
		allPushCmd:            allCmds,
		pushCmdDirMap:         pushCmdMap,
		otherPushCmd:          otherCmds,
		cmdErrorRepeatTime:    envRepeatTime,
		onPlay:                onPlay,
		onStop:                onStop,
		onPublish:             onPublish,
		onTeardown:            onTeardown,
	}
	if !server.localAuthorizationEnable && server.remoteHttpAuthorizationEnable && server.remoteHttpAuthorizationUrl == "" {
		logger.logger.Panicf("server configed remoteHttpAuthorizationEnable, but not set remoteHttpAuthorizationUrl")
	}
	return
}()

func GetServer() *Server {
	return Instance
}

func init() {
	var err error
	GetServer().mserver, err = InitializeMulticastServer()
	if Instance.enableMulticast {
		if Instance.multicastBindInf != nil {
			Instance.logger.Println("multicast bind interface:", Instance.multicastBindInf.Name)
		} else {
			Instance.logger.Println("multicast bind interface: <nil>")
		}
	}
	if err != nil {
		Instance.logger.Printf("InitializeMulticastServer error : %v", err)
	}
}

func (server *Server) Start() (err error) {
	var (
		logger   = server.logger
		addr     *net.TCPAddr
		listener *net.TCPListener
	)
	if addr, err = net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", server.TCPPort)); err != nil {
		return
	}
	if listener, err = net.ListenTCP("tcp", addr); err != nil {
		return
	}
	localRecord := server.localRecord             //utils.Conf().Section("rtsp").Key("save_stream_to_local").MustInt(0)
	ffmpeg := server.ffmpeg                       //utils.Conf().Section("rtsp").Key("ffmpeg_path").MustString("")
	m3u8_dir_path := server.m3u8DirPath           //utils.Conf().Section("rtsp").Key("m3u8_dir_path").MustString("")
	ts_duration_second := server.tsDurationSecond //utils.Conf().Section("rtsp").Key("ts_duration_second").MustInt(6)
	SaveStreamToLocal := false
	if (len(ffmpeg) > 0) && localRecord > 0 && len(m3u8_dir_path) > 0 {
		err = utils.EnsureDir(m3u8_dir_path)
		if err != nil {
			logger.Printf("Create m3u8_dir_path[%s] err:%v.", m3u8_dir_path, err)
		} else {
			SaveStreamToLocal = true
		}
	}
	go func() { // save to local.
		type pusherExecMap struct {
			lock   *sync.RWMutex
			cmdSet *hashset.Set
		}
		pusher2ffmpegMap := make(map[*Pusher]*CmdRepeatBag)
		pusher2CmdMap := make(map[*Pusher]*pusherExecMap)
		regx, _ := regexp.Compile("[ ]+")
		if SaveStreamToLocal {
			logger.Printf("Prepare to save stream to local....")
			defer logger.Printf("End save stream to local....")
		}
		var pusher *Pusher
		addChnOk := true
		removeChnOk := true
		for addChnOk || removeChnOk {
			select {
			case pusher, addChnOk = <-server.addPusherCh:
				if SaveStreamToLocal {
					if addChnOk {
						dir := path.Join(m3u8_dir_path, pusher.Path(), time.Now().Format("20060102-222222"))
						err := utils.EnsureDir(dir)
						if err != nil {
							logger.Printf("EnsureDir:[%s] err:%v.", dir, err)
							continue
						}
						m3u8path := path.Join(dir, fmt.Sprintf("out.m3u8"))
						port := pusher.Server().TCPPort
						rtsp := fmt.Sprintf("rtsp://localhost:%d%s", port, pusher.Path())
						paramStr := utils.Conf().Section("rtsp").Key(pusher.Path()).MustString("-c:v copy -c:a aac")
						params := []string{"-fflags", "genpts", "-rtsp_transport", "tcp", "-i", rtsp, "-hls_time", strconv.Itoa(ts_duration_second), "-hls_list_size", "0", m3u8path}
						if paramStr != "default" {
							paramsOfThisPath := strings.Split(paramStr, " ")
							params = append(params[:6], append(paramsOfThisPath, params[6:]...)...)
						}
						bag := NewCmdRepeatBag(ffmpeg, params, server.cmdErrorRepeatTime, pusher.Logger(), pusher.Path(), pusher.Session.ID)
						pusher2ffmpegMap[pusher] = bag
						bag.Run(func() {
							delete(pusher2ffmpegMap, pusher)
						})
						logger.Printf("add ffmpeg [%v] to pull stream from pusher[%v]", bag.Cmd, pusher)
					} else {
						logger.Printf("addPusherChan closed")
					}
				}
				if pusher != nil && pusher.MulticastClient == nil {
					pusherExecMap := &pusherExecMap{
						cmdSet: hashset.New(),
						lock:   &sync.RWMutex{},
					}
					path := strings.TrimLeft(pusher.Path(), "/")
					pathRegx, _ := regexp.Compile("[/]+")
					dir := "__default__"
					if dirs := pathRegx.Split(path, 2); len(dirs) > 1 {
						dir = dirs[0]
					}
					//运行指定一级路径ffmpeg命令
					for _, cmdRaw := range server.allPushCmd {
						cmd := strings.ReplaceAll(cmdRaw, "{path}", path)
						cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
						parametersRaw := regx.Split(cmd, -1)
						var parameters []string
						for _, parameter := range parametersRaw {
							if parameter != "" {
								parameters = append(parameters, parameter)
							}
						}
						bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, pusher.Logger(), pusher.Path(), pusher.Session.ID)
						pusherExecMap.lock.Lock()
						pusherExecMap.cmdSet.Add(bag)
						pusherExecMap.lock.Unlock()
						bag.Run(func() {
							pusherExecMap.lock.Lock()
							pusherExecMap.cmdSet.Remove(bag)
							pusherExecMap.lock.Unlock()
						})
					}
					if cmds := server.pushCmdDirMap[dir]; len(cmds) > 0 {
						//运行指定一级路径ffmpeg命令
						for _, cmdRaw := range cmds {
							cmd := strings.ReplaceAll(cmdRaw, "{path}", path)
							cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
							parametersRaw := regx.Split(cmd, -1)
							var parameters []string
							for _, parameter := range parametersRaw {
								if parameter != "" {
									parameters = append(parameters, parameter)
								}
							}
							bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, pusher.Logger(), pusher.Path(), pusher.Session.ID)
							pusherExecMap.lock.Lock()
							pusherExecMap.cmdSet.Add(bag)
							pusherExecMap.lock.Unlock()
							bag.Run(func() {
								pusherExecMap.lock.Lock()
								pusherExecMap.cmdSet.Remove(bag)
								pusherExecMap.lock.Unlock()
							})
						}
					} else if len(server.otherPushCmd) > 0 {
						//否则运行其他未指定的一级路径ffmpeg命令
						for _, cmdRaw := range server.otherPushCmd {
							cmd := strings.ReplaceAll(cmdRaw, "{path}", path)
							cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
							parametersRaw := regx.Split(cmd, -1)
							var parameters []string
							for _, parameter := range parametersRaw {
								if parameter != "" {
									parameters = append(parameters, parameter)
								}
							}
							bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, pusher.Logger(), pusher.Path(), pusher.Session.ID)
							pusherExecMap.lock.Lock()
							pusherExecMap.cmdSet.Add(bag)
							pusherExecMap.lock.Unlock()
							bag.Run(func() {
								pusherExecMap.lock.Lock()
								pusherExecMap.cmdSet.Remove(bag)
								pusherExecMap.lock.Unlock()
							})
						}
					}

					if pusherExecMap.cmdSet.Size() > 0 {
						pusher2CmdMap[pusher] = pusherExecMap
					}
				}
			case pusher, removeChnOk = <-server.removePusherCh:
				if SaveStreamToLocal {
					if removeChnOk {
						if bag := pusher2ffmpegMap[pusher]; bag != nil {
							bag.PushOver4Kill()
							delete(pusher2ffmpegMap, pusher)
							logger.Printf("delete ffmpeg from pull stream from pusher[%v]", pusher)
						}
					} else {
						for _pusher, bag := range pusher2ffmpegMap {
							if server.HasPusher(_pusher) {
								continue
							}
							bag.PushOver4Kill()
							delete(pusher2ffmpegMap, _pusher)
							logger.Printf("delete ffmpeg from pull stream from pusher[%v]", _pusher)
						}
						//pusher2ffmpegMap = make(map[*Pusher]*CmdRepeatBag)
						logger.Printf("removePusherChan closed")
					}
				}
				if pusher != nil && pusher.MulticastClient == nil {
					if execMap := pusher2CmdMap[pusher]; execMap != nil {
						for _, bagRaw := range execMap.cmdSet.Values() {
							bag := bagRaw.(*CmdRepeatBag)
							bag.PushOver4Kill()
						}
						delete(pusher2CmdMap, pusher)
					}
				} else if pusher == nil || !removeChnOk {
					for _pusher, execMap := range pusher2CmdMap {
						if server.HasPusher(_pusher) {
							continue
						}
						for _, bagRaw := range execMap.cmdSet.Values() {
							bag := bagRaw.(*CmdRepeatBag)
							bag.PushOver4Kill()
						}
						delete(pusher2CmdMap, _pusher)
					}
				}
			}
		}
	}()

	go func() {
		for !server.Stoped {
			select {
			case <-server.pushersCacheQueue:
				server.buildPushersCache()
			}
		}
	}()
	server.Stoped = false
	server.TCPListener = listener
	logger.Println("rtsp server start on", server.TCPPort)
	//networkBuffer := server.networkBuffer
	for !server.Stoped {
		var (
			conn *net.TCPConn
		)
		if conn, err = server.TCPListener.AcceptTCP(); err != nil {
			logger.Println(err)
			continue
		}
		_ = conn.SetNoDelay(true)
		_ = conn.SetKeepAlive(true)
		_ = conn.SetKeepAlivePeriod(time.Duration(10) * time.Second)
		_ = conn.SetWriteBuffer(int(maxRtpPackLen))
		//_ = conn.SetWriteBuffer(server.networkBuffer)
		//if err = conn.SetReadBuffer(networkBuffer); err != nil {
		//	logger.Printf("rtsp server conn set read buffer error, %v", err)
		//}
		//if err = conn.SetWriteBuffer(networkBuffer); err != nil {
		//	logger.Printf("rtsp server conn set write buffer error, %v", err)
		//}
		session := NewSession(server, conn)
		go session.Start()
	}
	return
}

func (server *Server) Stop() {
	logger := server.logger
	logger.Println("rtsp server stop on", server.TCPPort)
	server.Stoped = true
	if server.TCPListener != nil {
		server.TCPListener.Close()
		server.TCPListener = nil
	}
	server.pushers = &sync.Map{}
	if server.enableMulticast {
		server.mserver.stopped = true
	}
	close(server.addPusherCh)
	close(server.removePusherCh)
}

func (server *Server) AddPusher(pusher *Pusher) bool {
	logger := pusher.Logger()
	added := false
	_, ok := server.pushers.Load(pusher.Path())
	if !ok {
		server.pushers.Store(pusher.Path(), pusher)
		logger.Printf("%v start", pusher)
		added = true
	} else {
		added = false
	}
	if added {
		go pusher.Start()
		server.addPusherCh <- pusher
		server.genServerRebuildPusherCacheSingle()
		if GetServer().EnableAudioHttpStream {
			pusher.udpHttpAudioStreamListener = NewMp3UdpDataListener(pusher)
			if err := pusher.udpHttpAudioStreamListener.Start(); err != nil {
				logger.Printf("start mp3 udp listen error:%v", err)
			}
		}
		//if GetServer().EnableVideoHttpStream {
		//	pusher.udpHttpVideoStreamListener = NewMp4UdpDataListener(pusher)
		//	if err := pusher.udpHttpVideoStreamListener.Start(); err != nil {
		//		logger.Printf("start mp4 udp listen error:%v", err)
		//	}
		//}
	}
	return added
}

func (server *Server) HasPusher(pusher *Pusher) bool {
	_, ok := server.pushers.Load(pusher.Path())
	return ok
}

func (server *Server) genServerRebuildPusherCacheSingle() {
	select {
	case server.pushersCacheQueue <- true:
	default:
	}
}

func (server *Server) TryAttachToPusher(session *Session) (int, *Pusher) {
	attached := 0
	var pusher *Pusher = nil
	if __pusher, ok := server.pushers.Load(session.Path); ok {
		_pusher := __pusher.(*Pusher)
		if _pusher.RebindSession(session) {
			session.logger.Printf("Attached to a pusher")
			attached = 1
			pusher = _pusher
		} else {
			attached = -1
		}
	}
	return attached, pusher
}

func (server *Server) RemovePusher(pusher *Pusher) {
	removed := false
	if _pusher, ok := server.pushers.Load(pusher.Path()); ok && pusher.ID() == _pusher.(*Pusher).ID() {
		server.pushers.Delete(pusher.Path())
		pusher.Logger().Printf("%v end, pusher[%v]\n", pusher, _pusher.(*Pusher).Path())
		removed = true
	}
	if removed {
		server.removePusherCh <- pusher
		server.genServerRebuildPusherCacheSingle()
	}
}

//获取推流
func (server *Server) GetPusher(path string) (pusher *Pusher) {
	pusher, _ = server.pushersCache[path]
	return
}

func (server *Server) buildPushersCache() {
	pushers := make(map[string]*Pusher)
	server.pushers.Range(func(key, value interface{}) bool {
		pushers[key.(string)] = value.(*Pusher)
		return true
	})
	server.pushersCache = pushers
}

func (server *Server) GetPushers() (pushers map[string]*Pusher) {
	return server.pushersCache
}

func (server *Server) GetPusherSize() (size int) {
	return len(server.pushersCache)
}
