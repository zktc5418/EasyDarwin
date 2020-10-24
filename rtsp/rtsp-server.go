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
	TCPListener            *net.TCPListener
	TCPPort                int
	Stoped                 bool
	pushers                map[string]*Pusher // Path <-> Pusher
	pushersLock            sync.RWMutex
	addPusherCh            chan *Pusher
	removePusherCh         chan *Pusher
	rtpMinUdpPort          uint16
	rtpMaxUdpPort          uint16
	networkBuffer          int
	localRecord            byte
	ffmpeg                 string
	m3u8DirPath            string
	tsDurationSecond       int
	gopCacheEnable         bool
	debugLogEnable         bool
	playerQueueLimit       int
	dropPacketWhenPaused   bool
	rtspTimeoutMillisecond int
	authorizationEnable    bool
	closeOld               bool
	svcDiscoverMultiAddr   string
	svcDiscoverMultiPort   uint16
	enableMulticast        bool
	multicastAddr          string
	multicastBindInf       *net.Interface
	mserver                *MulticastServer
	pushCmd                []string
	cmdErrorRepeatTime     uint8
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
	ffmpeg := rtspFile.Key("ffmpeg_path").MustString("")
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
	var cmds []string
	environs := os.Environ()
	envKey := "EASYDARWIN_PUSH_FFMPEG_CMD="
	repeatTimeEnvKey := "EASYDARWIN_CMD_ERROR_REPEAT_TIME="
	var envRepeatTime uint8
	for _, environ := range environs {
		if strings.HasPrefix(environ, envKey) {
			envVal := environ[len(envKey):]
			cmds = append(cmds, strings.Split(envVal, ";")...)
		}
		if strings.HasPrefix(environ, repeatTimeEnvKey) {
			envVal := environ[len(envKey):]
			if intRaw, err := strconv.ParseUint(envVal, 0, 8); err == nil {
				envRepeatTime = uint8(intRaw)
			}
		}
	}
	if len(cmds) == 0 {
		cmdKeys := utils.Conf().Section("cmd").Keys()
		for _, key := range cmdKeys {
			if strings.HasPrefix(key.Name(), "execute_") {
				cmds = append(cmds, key.Value())
			}
		}
	}
	if len(cmds) > 0 {
		logger.logger.Printf("pusher cmds: \n %s", strings.Join(cmds, "\n"))
	}
	if envRepeatTime == 0 {
		envRepeatTime = uint8(utils.Conf().Section("cmd").Key("cmd_error_repeat_time").MustUint(5))
	}
	server = &Server{
		SessionLogger:          logger,
		Stoped:                 true,
		TCPPort:                rtspFile.Key("port").MustInt(554),
		pushers:                make(map[string]*Pusher),
		addPusherCh:            make(chan *Pusher),
		removePusherCh:         make(chan *Pusher),
		rtpMinUdpPort:          uint16(rtpMinPort),
		rtpMaxUdpPort:          uint16(rtpMaxPort),
		networkBuffer:          networkBuffer,
		localRecord:            byte(localRecord),
		ffmpeg:                 ffmpeg,
		m3u8DirPath:            m3u8_dir_path,
		tsDurationSecond:       ts_duration_second,
		gopCacheEnable:         rtspFile.Key("gop_cache_enable").MustBool(true),
		debugLogEnable:         rtspFile.Key("debug_log_enable").MustBool(false),
		playerQueueLimit:       rtspFile.Key("player_queue_limit").MustInt(0),
		dropPacketWhenPaused:   rtspFile.Key("drop_packet_when_paused").MustBool(false),
		rtspTimeoutMillisecond: rtspFile.Key("timeout").MustInt(0),
		authorizationEnable:    rtspFile.Key("authorization_enable").MustBool(false),
		closeOld:               rtspFile.Key("close_old").MustBool(false),
		svcDiscoverMultiAddr:   rtspFile.Key("svc_discover_multiaddr").MustString("239.12.12.12"),
		svcDiscoverMultiPort:   uint16(rtspFile.Key("svc_discover_multiport").MustUint(1212)),
		enableMulticast:        rtspFile.Key("enable_multicast").MustBool(false),
		multicastAddr:          rtspFile.Key("multicast_svc_discover_addr").MustString("232.2.2.2:8760"),
		multicastBindInf:       multicastBindInf,
		pushCmd:                cmds,
		cmdErrorRepeatTime:     envRepeatTime,
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
	logger := server.logger
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", server.TCPPort))
	if err != nil {
		return
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return
	}

	localRecord := server.localRecord             //utils.Conf().Section("rtsp").Key("save_stream_to_local").MustInt(0)
	ffmpeg := server.ffmpeg                       //utils.Conf().Section("rtsp").Key("ffmpeg_path").MustString("")
	m3u8_dir_path := server.m3u8DirPath           //utils.Conf().Section("rtsp").Key("m3u8_dir_path").MustString("")
	ts_duration_second := server.tsDurationSecond //utils.Conf().Section("rtsp").Key("ts_duration_second").MustInt(6)
	SaveStreamToLocal := false
	if (len(ffmpeg) > 0) && localRecord > 0 && len(m3u8_dir_path) > 0 {
		err := utils.EnsureDir(m3u8_dir_path)
		if err != nil {
			logger.Printf("Create m3u8_dir_path[%s] err:%v.", m3u8_dir_path, err)
		} else {
			SaveStreamToLocal = true
		}
	}
	go func() { // save to local.
		pusher2ffmpegMap := make(map[*Pusher]*CmdRepeatBag)
		pusher2CmdMap := make(map[*Pusher]*hashset.Set)
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
						dir := path.Join(m3u8_dir_path, pusher.Path(), time.Now().Format("20060102"))
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
						// ffmpeg -i ~/Downloads/720p.mp4 -s 640x360 -g 15 -c:a aac -hls_time 5 -hls_list_size 0 record.m3u8
						//cmd := exec.Command(ffmpeg, params...)
						//f, err := os.OpenFile(path.Join(dir, fmt.Sprintf("log.txt")), os.O_RDWR|os.O_CREATE, 0755)
						//if err == nil {
						//	cmd.Stdout = f
						//	cmd.Stderr = f
						//}
						//err = cmd.Start()
						//if err != nil {
						//	logger.Printf("Start ffmpeg err:%v", err)
						//}
						//go func() {
						//	//防止转码进程自己因为错误停止，出现僵尸进程
						//	if err2 := cmd.Wait(); err2 != nil {
						//		logger.Printf("exit  process error:%v", err2)
						//	}
						//	time.Sleep(time.Duration(2) * time.Second)
						//	delete(pusher2ffmpegFileMap, pusher)
						//	delete(pusher2ffmpegMap, pusher)
						//}()
						bag := NewCmdRepeatBag(ffmpeg, params, server.cmdErrorRepeatTime, server.logger)
						pusher2ffmpegMap[pusher] = bag
						bag.Run(func() {
							delete(pusher2ffmpegMap, pusher)
						})
						logger.Printf("add ffmpeg [%v] to pull stream from pusher[%v]", bag.Cmd, pusher)
					} else {
						logger.Printf("addPusherChan closed")
					}
				}
				if len(server.pushCmd) > 0 && pusher.MulticastClient == nil {
					cmdSet := hashset.New()
					//var cmds []*exec.Cmd
					//var openFiles []*os.File
					for _, cmdRaw := range server.pushCmd {
						cmd := strings.ReplaceAll(cmdRaw, "{path}", strings.TrimLeft(pusher.Path(), "/"))
						cmd = strings.TrimLeft(strings.TrimSpace(cmd), "ffmpeg")
						parametersRaw := regx.Split(cmd, -1)
						var parameters []string
						for _, parameter := range parametersRaw {
							if parameter != "" {
								parameters = append(parameters, parameter)
							}
						}
						//execCmd := exec.Command(server.ffmpeg, parameters...)
						//f, err := os.OpenFile(path.Join(utils.CWD(), fmt.Sprintf("exec.log")), os.O_RDWR|os.O_CREATE, 0755)
						//if err == nil {
						//	execCmd.Stdout = f
						//	execCmd.Stderr = f
						//}
						//err = execCmd.Start()
						//if err != nil {
						//	logger.Printf("Start exec err:%v", err)
						//}
						//cmdSet.Add(execCmd)
						//openFileSet.Add(f)
						////cmds = append(cmds, execCmd)
						////openFiles = append(openFiles, f)
						//go func() {
						//	//防止转码进程自己因为错误停止，出现僵尸进程
						//	if err2 := execCmd.Wait(); err2 != nil {
						//		logger.Printf("exit  process error:%v", err2)
						//	}
						//	time.Sleep(time.Duration(2) * time.Second)
						//	cmdSet.Remove(execCmd)
						//	openFileSet.Remove(f)
						//}()
						bag := NewCmdRepeatBag(server.ffmpeg, parameters, server.cmdErrorRepeatTime, server.logger)
						cmdSet.Add(bag)
						bag.Run(func() {
							cmdSet.Remove(bag)
						})
					}
					pusher2CmdMap[pusher] = cmdSet
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
						for _, bag := range pusher2ffmpegMap {
							bag.PusherTerminated = true
							err2 := bag.Cmd.Process.Kill()
							logger.Printf("kill  process error:%v", err2)
						}
						pusher2ffmpegMap = make(map[*Pusher]*CmdRepeatBag)
						logger.Printf("removePusherChan closed")
					}
				}
				if len(server.pushCmd) > 0 && pusher.MulticastClient == nil {
					for _, bagRaw := range pusher2CmdMap[pusher].Values() {
						bag := bagRaw.(*CmdRepeatBag)
						bag.PushOver4Kill()
					}
					delete(pusher2CmdMap, pusher)
				}
			}
		}
	}()

	server.Stoped = false
	server.TCPListener = listener
	logger.Println("rtsp server start on", server.TCPPort)
	networkBuffer := server.networkBuffer
	for !server.Stoped {
		conn, err := server.TCPListener.Accept()
		if err != nil {
			logger.Println(err)
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			if err := tcpConn.SetReadBuffer(networkBuffer); err != nil {
				logger.Printf("rtsp server conn set read buffer error, %v", err)
			}
			if err := tcpConn.SetWriteBuffer(networkBuffer); err != nil {
				logger.Printf("rtsp server conn set write buffer error, %v", err)
			}
		}
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
	server.pushersLock.Lock()
	server.pushers = make(map[string]*Pusher)
	server.pushersLock.Unlock()

	close(server.addPusherCh)
	close(server.removePusherCh)
}

func (server *Server) AddPusher(pusher *Pusher) bool {
	logger := server.logger
	added := false
	server.pushersLock.Lock()
	_, ok := server.pushers[pusher.Path()]
	if !ok {
		server.pushers[pusher.Path()] = pusher
		logger.Printf("%v start, now pusher size[%d]", pusher, len(server.pushers))
		added = true
	} else {
		added = false
	}
	server.pushersLock.Unlock()
	if added {
		go pusher.Start()
		server.addPusherCh <- pusher
	}
	return added
}

func (server *Server) TryAttachToPusher(session *Session) (int, *Pusher) {
	server.pushersLock.Lock()
	attached := 0
	var pusher *Pusher = nil
	if _pusher, ok := server.pushers[session.Path]; ok {
		if _pusher.RebindSession(session) {
			session.logger.Printf("Attached to a pusher")
			attached = 1
			pusher = _pusher
		} else {
			attached = -1
		}
	}
	server.pushersLock.Unlock()
	return attached, pusher
}

func (server *Server) RemovePusher(pusher *Pusher) {
	logger := server.logger
	removed := false
	server.pushersLock.Lock()
	if _pusher, ok := server.pushers[pusher.Path()]; ok && pusher.ID() == _pusher.ID() {
		delete(server.pushers, pusher.Path())
		logger.Printf("%v end, now pusher size[%d]\n", pusher, len(server.pushers))
		removed = true
	}
	server.pushersLock.Unlock()
	if removed {
		server.removePusherCh <- pusher
	}
}

//获取推流
func (server *Server) GetPusher(path string) (pusher *Pusher) {
	server.pushersLock.RLock()
	pusher = server.pushers[path]
	server.pushersLock.RUnlock()
	return
}

func (server *Server) GetPushers() (pushers map[string]*Pusher) {
	pushers = make(map[string]*Pusher)
	server.pushersLock.RLock()
	for k, v := range server.pushers {
		pushers[k] = v
	}
	server.pushersLock.RUnlock()
	return
}

func (server *Server) GetPusherSize() (size int) {
	server.pushersLock.RLock()
	size = len(server.pushers)
	server.pushersLock.RUnlock()
	return
}
