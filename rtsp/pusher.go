package rtsp

import (
	"log"
	"strings"
	"sync"
	"time"
)

const MAX_GOP_CACHE_LEN uint = 256

type Pusher struct {
	*Session
	*RTSPClient
	*MulticastClient
	players        map[string]*Player //SessionID <-> Player
	playersLock    sync.RWMutex
	gopCacheEnable bool

	gopCache []*RTPPack
	//gopCacheLock      sync.RWMutex
	UDPServer         *UDPServer
	spsppsInSTAPaPack bool
	//cond              *sync.Cond
	queue chan *RTPPack
}

func (pusher *Pusher) String() string {
	if pusher.Session != nil {
		return pusher.Session.String()
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.String()
	}
	return pusher.RTSPClient.String()
}

func (pusher *Pusher) Server() *Server {
	if pusher.Session != nil {
		return pusher.Session.Server
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.Server
	}
	return pusher.RTSPClient.Server
}

func (pusher *Pusher) SDPRaw() string {
	if pusher.Session != nil {
		return pusher.Session.SDPRaw
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.SDPRaw
	}
	return pusher.RTSPClient.SDPRaw
}

func (pusher *Pusher) Stoped() bool {
	if pusher.Session != nil {
		return pusher.Session.Stoped
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.Stopped
	}
	return pusher.RTSPClient.Stoped
}

func (pusher *Pusher) Path() string {
	if pusher.Session != nil {
		return pusher.Session.Path
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.Path
	}
	if pusher.RTSPClient.CustomPath != "" {
		return pusher.RTSPClient.CustomPath
	}
	return pusher.RTSPClient.Path
}

func (pusher *Pusher) ID() string {
	if pusher.Session != nil {
		return pusher.Session.ID
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.SourceSessionId
	}
	return pusher.RTSPClient.ID
}

func (pusher *Pusher) Logger() *log.Logger {
	if pusher.Session != nil {
		return pusher.Session.logger
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.logger
	}
	return pusher.RTSPClient.logger
}

func (pusher *Pusher) VCodec() string {
	if pusher.Session != nil {
		return pusher.Session.VCodec
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.VCodec
	}
	return pusher.RTSPClient.VCodec
}

func (pusher *Pusher) ACodec() string {
	if pusher.Session != nil {
		return pusher.Session.ACodec
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.ACodec
	}
	return pusher.RTSPClient.ACodec
}

func (pusher *Pusher) AControl() string {
	if pusher.Session != nil {
		return pusher.Session.AControl
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.AControl
	}
	return pusher.RTSPClient.AControl
}

func (pusher *Pusher) VControl() string {
	if pusher.Session != nil {
		return pusher.Session.VControl
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.VControl
	}
	return pusher.RTSPClient.VControl
}

func (pusher *Pusher) URL() string {
	if pusher.Session != nil {
		return pusher.Session.URL
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.SourceUrl
	}
	return pusher.RTSPClient.URL
}

func (pusher *Pusher) AddOutputBytes(size int) {
	if pusher.Session != nil {
		pusher.Session.OutBytes += size
		return
	}
	if pusher.MulticastClient != nil {
		pusher.MulticastClient.OutBytes += size
		return
	}
	pusher.RTSPClient.OutBytes += size
}

func (pusher *Pusher) InBytes() int {
	if pusher.Session != nil {
		return pusher.Session.InBytes
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.InBytes
	}
	return pusher.RTSPClient.InBytes
}

func (pusher *Pusher) OutBytes() int {
	if pusher.Session != nil {
		return pusher.Session.OutBytes
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.OutBytes
	}
	return pusher.RTSPClient.OutBytes
}

func (pusher *Pusher) TransType() string {
	if pusher.Session != nil {
		return pusher.Session.TransType.String()
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.TransType.String()
	}
	return pusher.RTSPClient.TransType.String()
}

func (pusher *Pusher) StartAt() time.Time {
	if pusher.Session != nil {
		return pusher.Session.StartAt
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.StartAt
	}
	return pusher.RTSPClient.StartAt
}

func (pusher *Pusher) Source() string {
	if pusher.Session != nil {
		return pusher.Session.URL
	}
	if pusher.MulticastClient != nil {
		return pusher.MulticastClient.multiInfo.SourceUrl
	}
	return pusher.RTSPClient.URL
}

/**
后端手动添加rtsp 拉流
*/
func NewClientPusher(client *RTSPClient) (pusher *Pusher) {
	pusher = &Pusher{
		RTSPClient:     client,
		Session:        nil,
		players:        make(map[string]*Player),
		gopCacheEnable: GetServer().gopCacheEnable,
		gopCache:       make([]*RTPPack, 0),

		//cond:  sync.NewCond(&sync.Mutex{}),
		queue: make(chan *RTPPack, MAX_GOP_CACHE_LEN),
	}
	client.RTPHandles = append(client.RTPHandles, func(pack *RTPPack) {
		pusher.QueueRTP(pack)
	})
	client.StopHandles = append(client.StopHandles, func() {
		pusher.ClearPlayer()
		pusher.Server().RemovePusher(pusher)
		//pusher.cond.Broadcast()
	})
	//
	server := client.Server
	if server.enableMulticast {
		client.RTPHandles = append(client.RTPHandles, func(pack *RTPPack) {
			//发送rtp组播包
			if server.mserver != nil {
				server.mserver.SendMulticastRtpPack(pack, client.multicastInfo)
			}
		}, func(pack *RTPPack) {
			//发送推流服务通信包
			multiCommand := &MulticastCommand{
				Command:   START_MULTICAST,
				MultiInfo: client.multicastInfo,
			}
			send := false
			if client.multicastBoardTimes == -1 {
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			} else if client.multicastBoardTimes < 5 && client.multicastLastBoard.Add(time.Duration(1<<client.multicastBoardTimes)*time.Second).Before(time.Now()) {
				//前五次发送指数间隔指数递增
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			} else if client.multicastLastBoard.Add(time.Duration(20) * time.Second).Before(time.Now()) {
				//后续每隔20秒发送一次
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			}
			if send {
				client.multicastBoardTimes++
				client.multicastLastBoard = time.Now()
			}
		})

		client.StopHandles = append(client.StopHandles, func() {
			go func() {
				//发送推流服务通信包
				multiCommand := &MulticastCommand{
					Command:   STOP_MULTICAST,
					MultiInfo: client.multicastInfo,
				}
				for i := -1; i < 10; i++ {
					if i > -1 && i < 4 {
						time.Sleep(time.Duration(1<<i) * time.Second)
					} else if i >= 4 {
						time.Sleep(time.Duration(10) * time.Second)
					}
					if server.GetPusher(client.Path) != nil {
						//有新的相同的推流地址，停止发送停止推流命令，并退出
						break
					}
					server.mserver.SendMulticastCommandData(multiCommand)
				}
			}()
		})
	}
	return
}

func NewMulticastPusher(multiInfo *MulticastCommunicateInfo) (pusher *Pusher) {
	pusher = &Pusher{
		RTSPClient:     nil,
		players:        make(map[string]*Player),
		gopCacheEnable: GetServer().gopCacheEnable,
		gopCache:       make([]*RTPPack, 0),

		//cond:  sync.NewCond(&sync.Mutex{}),
		queue: make(chan *RTPPack, MAX_GOP_CACHE_LEN),
	}
	multicastClient, _ := StartMulticastListen(pusher, multiInfo)
	pusher.MulticastClient = multicastClient
	multicastClient.RTPHandles = append(multicastClient.RTPHandles, func(pack *RTPPack) {
		pusher.QueueRTP(pack)
	})
	multicastClient.StopHandles = append(multicastClient.StopHandles, func() {
		pusher.ClearPlayer()
		pusher.Server().RemovePusher(pusher)
	})
	return pusher
}

//rtsp推流
func NewPusher(session *Session) (pusher *Pusher) {
	pusher = &Pusher{
		Session:        session,
		RTSPClient:     nil,
		players:        make(map[string]*Player),
		gopCacheEnable: GetServer().gopCacheEnable,
		gopCache:       make([]*RTPPack, 0),

		//cond:  sync.NewCond(&sync.Mutex{}),
		queue: make(chan *RTPPack, MAX_GOP_CACHE_LEN),
	}
	pusher.bindSession(session)
	return
}

func (pusher *Pusher) bindSession(session *Session) {
	pusher.Session = session
	session.RTPHandles = append(session.RTPHandles, func(pack *RTPPack) {
		if session != pusher.Session {
			session.logger.Printf("Session recv rtp to pusher.but pusher got a new session[%v].", pusher.Session.ID)
			return
		}
		pusher.QueueRTP(pack)
	})
	session.StopHandles = append(session.StopHandles, func() {
		if session != pusher.Session {
			session.logger.Printf("Session stop to release pusher.but pusher got a new session[%v].", pusher.Session.ID)
			return
		}
		pusher.ClearPlayer()
		pusher.Server().RemovePusher(pusher)
		//pusher.cond.Broadcast()
		if pusher.UDPServer != nil {
			pusher.UDPServer.Stop()
			pusher.UDPServer = nil
		}
	})

	//组播数据推送
	server := session.Server
	if server.enableMulticast {
		//绑定组播信息
		multicastInfo := &MulticastCommunicateInfo{
			SDPRaw:          session.SDPRaw,
			SourceUrl:       session.URL,
			Path:            session.Path,
			SourceSessionId: session.ID,
		}
		session.multicastInfo = multicastInfo
		if session.AControl != "" {
			session.multicastInfo.AudioRtpMultiAddress, session.multicastInfo.AudioRtpPort = RandomMulticastAddress()
			session.multicastInfo.CtlAudioRtpMultiAddress, session.multicastInfo.CtlAudioRtpPort = RandomMulticastAddress()
		}
		if session.VControl != "" {
			session.multicastInfo.VideoRtpMultiAddress, session.multicastInfo.VideoRtpPort = RandomMulticastAddress()
			session.multicastInfo.CtlVideoRtpMultiAddress, session.multicastInfo.CtlVideoRtpPort = RandomMulticastAddress()
		}

		session.RTPHandles = append(session.RTPHandles, func(pack *RTPPack) {
			//发送rtp组播包
			if server.mserver != nil {
				server.mserver.SendMulticastRtpPack(pack, session.multicastInfo)
			}
		}, func(pack *RTPPack) {
			//发送推流服务通信包
			multiCommand := &MulticastCommand{
				Command:   START_MULTICAST,
				MultiInfo: session.multicastInfo,
			}
			send := false
			if session.multicastBoardTimes == -1 {
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			} else if session.multicastBoardTimes < 5 && session.multicastLastBoard.Add(time.Duration(1<<session.multicastBoardTimes)*time.Second).Before(time.Now()) {
				//前五次发送指数间隔指数递增
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			} else if session.multicastLastBoard.Add(time.Duration(20) * time.Second).Before(time.Now()) {
				//后续每隔20秒发送一次
				server.mserver.SendMulticastCommandData(multiCommand)
				send = true
			}
			if send {
				session.multicastBoardTimes++
				session.multicastLastBoard = time.Now()
			}
		})

		session.StopHandles = append(session.StopHandles, func() {
			go func() {
				//发送推流服务通信包
				multiCommand := &MulticastCommand{
					Command:   STOP_MULTICAST,
					MultiInfo: session.multicastInfo,
				}
				for i := -1; i < 10; i++ {
					if i > -1 && i < 4 {
						time.Sleep(time.Duration(1<<i) * time.Second)
					} else if i >= 4 {
						time.Sleep(time.Duration(10) * time.Second)
					}
					if server.GetPusher(session.Path) != nil {
						//有新的相同的推流地址，停止发送停止推流命令，并退出
						break
					}
					server.mserver.SendMulticastCommandData(multiCommand)
				}
			}()
		})
	}
}

func (pusher *Pusher) RebindSession(session *Session) bool {
	if pusher.RTSPClient != nil {
		pusher.Logger().Printf("call RebindSession[%s] to a Client-Pusher. got false", session.ID)
		return false
	}
	sess := pusher.Session
	pusher.bindSession(session)
	session.Pusher = pusher

	//pusher.gopCacheLock.Lock()
	pusher.gopCache = pusher.gopCache[0:0] //make([]*RTPPack, 0)
	//pusher.gopCacheLock.Unlock()
	if sess != nil {
		sess.Stop()
	}
	return true
}

func (pusher *Pusher) RebindClient(client *RTSPClient) bool {
	if pusher.Session != nil {
		pusher.Logger().Printf("call RebindClient[%s] to a Session-Pusher. got false", client.ID)
		return false
	}
	sess := pusher.RTSPClient
	pusher.RTSPClient = client
	if sess != nil {
		sess.Stop()
	}
	return true
}

func (pusher *Pusher) QueueRTP(pack *RTPPack) *Pusher {
	//pusher.cond.L.Lock()
	pusher.queue <- pack
	//pusher.queue = append(pusher.queue, pack)
	//pusher.cond.Signal()
	//pusher.cond.L.Unlock()
	return pusher
}

func (pusher *Pusher) Start() {
	logger := pusher.Logger()
	for !pusher.Stoped() {
		var pack *RTPPack
		pack, ok := <-pusher.queue
		//pusher.cond.L.Lock()
		//if len(pusher.queue) == 0 {
		//	pusher.cond.Wait()
		//}
		//if len(pusher.queue) > 0 {
		//	pack = pusher.queue[0]
		//	pusher.queue = pusher.queue[1:]
		//}
		//pusher.cond.L.Unlock()
		if pack == nil || !ok {
			if !pusher.Stoped() {
				logger.Printf("pusher not stoped, but queue take out nil pack")
			}
			continue
		}

		if pusher.gopCacheEnable && pack.Type == RTP_TYPE_VIDEO {
			//pusher.gopCacheLock.Lock()
			if rtp := ParseRTP(pack.Buffer.Bytes()); rtp != nil && pusher.shouldSequenceStart(rtp) {
				pusher.gopCache = pusher.gopCache[0:0]
			}
			if l := len(pusher.gopCache); uint(l) >= MAX_GOP_CACHE_LEN {
				pusher.gopCache = pusher.gopCache[0:0]
			}
			pusher.gopCache = append(pusher.gopCache, pack)
			//pusher.gopCacheLock.Unlock()
		}
		pusher.BroadcastRTP(pack)
	}
}

func (pusher *Pusher) Stop() {
	defer func() {
		if err := recover(); err != nil {
			pusher.Session.logger.Printf("stop pusher error:%v", err)
		}
	}()
	if pusher.Stoped() {
		return
	}
	close(pusher.queue)
	if pusher.Session != nil {
		pusher.Session.Stop()
		return
	}
	if pusher.MulticastClient != nil {
		pusher.MulticastClient.Stop()
		return
	}
	pusher.RTSPClient.Stop()
}

func (pusher *Pusher) BroadcastRTP(pack *RTPPack) *Pusher {
	for _, player := range pusher.GetPlayers() {
		player.QueueRTP(pack)
		pusher.AddOutputBytes(pack.Buffer.Len())
	}
	return pusher
}

func (pusher *Pusher) GetPlayers() (players map[string]*Player) {
	players = make(map[string]*Player)
	pusher.playersLock.RLock()
	for k, v := range pusher.players {
		players[k] = v
	}
	pusher.playersLock.RUnlock()
	return
}

func (pusher *Pusher) HasPlayer(player *Player) bool {
	pusher.playersLock.RLock()
	_, ok := pusher.players[player.ID]
	pusher.playersLock.RUnlock()
	return ok
}

func (pusher *Pusher) AddPlayer(player *Player) *Pusher {
	logger := pusher.Logger()
	pusher.playersLock.Lock()
	if _, ok := pusher.players[player.ID]; !ok {
		pusher.players[player.ID] = player
		go player.Start()
		logger.Printf("%v start, now player size[%d]", player, len(pusher.players))
	}
	pusher.playersLock.Unlock()
	if pusher.gopCacheEnable {
		//pusher.gopCacheLock.RLock()
		packs := pusher.gopCache[:]
		for _, pack := range packs {
			player.QueueRTP(pack)
			pusher.AddOutputBytes(pack.Buffer.Len())
		}
		//pusher.gopCacheLock.RUnlock()
	}
	return pusher
}

func (pusher *Pusher) RemovePlayer(player *Player) *Pusher {
	logger := pusher.Logger()
	pusher.playersLock.Lock()
	if len(pusher.players) == 0 {
		pusher.playersLock.Unlock()
		return pusher
	}
	delete(pusher.players, player.ID)
	logger.Printf("%v end, now player size[%d]\n", player, len(pusher.players))
	pusher.playersLock.Unlock()
	return pusher
}

func (pusher *Pusher) ClearPlayer() {
	// copy a new map to avoid deadlock
	players := pusher.players
	pusher.playersLock.Lock()
	//for k, v := range pusher.players {
	//	//v.Stop()
	//	players[k] = v
	//}
	pusher.players = make(map[string]*Player)
	pusher.playersLock.Unlock()
	go func() { // do not block
		for _, v := range players {
			v.Stop()
		}
	}()
}

func (pusher *Pusher) shouldSequenceStart(rtp *RTPInfo) bool {
	if strings.EqualFold(pusher.VCodec(), "h264") {
		var realNALU uint8
		payloadHeader := rtp.Payload[0] //https://tools.ietf.org/html/rfc6184#section-5.2
		NaluType := uint8(payloadHeader & 0x1F)
		// log.Printf("RTP Type:%d", NaluType)
		switch {
		case NaluType <= 23:
			realNALU = rtp.Payload[0]
			// log.Printf("Single NAL:%d", NaluType)
		case NaluType == 28 || NaluType == 29:
			realNALU = rtp.Payload[1]
			if realNALU&0x40 != 0 {
				// log.Printf("FU NAL End :%02X", realNALU)
			}
			if realNALU&0x80 != 0 {
				// log.Printf("FU NAL Begin :%02X", realNALU)
			} else {
				return false
			}
		case NaluType == 24:
			// log.Printf("STAP-A")
			off := 1
			singleSPSPPS := 0
			for {
				nalSize := ((uint16(rtp.Payload[off])) << 8) | uint16(rtp.Payload[off+1])
				if nalSize < 1 || len(rtp.Payload) < off+int(nalSize) {
					return false
				}
				off += 2
				nalUnit := rtp.Payload[off : off+int(nalSize)]
				off += int(nalSize)
				realNALU = nalUnit[0]
				singleSPSPPS += int(realNALU & 0x1F)
				if off >= len(rtp.Payload) {
					break
				}
			}
			if singleSPSPPS == 0x0F {
				pusher.spsppsInSTAPaPack = true
				return true
			}
		}
		if realNALU&0x1F == 0x05 {
			if pusher.spsppsInSTAPaPack {
				return false
			}
			return true
		}
		if realNALU&0x1F == 0x07 { // maybe sps pps header + key frame?
			if len(rtp.Payload) < 200 { // consider sps pps header only.
				return true
			}
			return true
		}
		return false
	} else if strings.EqualFold(pusher.VCodec(), "h265") {
		if len(rtp.Payload) >= 3 {
			firstByte := rtp.Payload[0]
			headerType := (firstByte >> 1) & 0x3f
			var frameType uint8
			if headerType == 49 { //Fragmentation Units

				FUHeader := rtp.Payload[2]
				/*
				   +---------------+
				   |0|1|2|3|4|5|6|7|
				   +-+-+-+-+-+-+-+-+
				   |S|E|  FuType   |
				   +---------------+
				*/
				rtpStart := (FUHeader & 0x80) != 0
				if !rtpStart {
					if (FUHeader & 0x40) != 0 {
						//log.Printf("FU frame end")
					}
					return false
				} else {
					//log.Printf("FU frame start")
				}
				frameType = FUHeader & 0x3f
			} else if headerType == 48 { //Aggregation Packets

			} else if headerType == 50 { //PACI Packets

			} else { // Single NALU
				/*
					+---------------+---------------+
					|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
					+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
					|F|   Type    |  LayerId  | TID |
					+-------------+-----------------+
				*/
				frameType = firstByte & 0x7e
			}
			if frameType >= 16 && frameType <= 21 {
				return true
			}
			if frameType == 32 {
				// vps sps pps...
				if len(rtp.Payload) < 200 { // consider sps pps header only.
					return false
				}
				return true
			}
		}
		return false
	}
	return false
}
