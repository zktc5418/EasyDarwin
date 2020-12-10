package rtsp

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type CmdRepeatBag struct {
	Cmd              *exec.Cmd
	errorTime        uint8
	PusherTerminated bool
	MaxRepeatTime    uint8
	cmdName          string
	args             []string
	logger           *log.Logger
	pusherPath       string
	sessionId        string
}

func NewCmdRepeatBag(cmdName string, args []string, MaxRepeatTime uint8, logger *log.Logger, path string, sessionId string) (cmdBag *CmdRepeatBag) {
	cmdBag = &CmdRepeatBag{
		errorTime:        0,
		PusherTerminated: false,
		MaxRepeatTime:    MaxRepeatTime,
		cmdName:          cmdName,
		args:             args,
		logger:           logger,
		pusherPath:       path,
		sessionId:        sessionId,
	}
	return
}

func (bag CmdRepeatBag) Run(overErrorTimeCallBack func()) {

	bag.logger.Printf("excute ffmpeg command:%s", strings.Join(append([]string{bag.cmdName}, bag.args...), " "))
	bag.Cmd = exec.Command(bag.cmdName, bag.args...)
	bag.Cmd.Stdout = os.Stdout
	bag.Cmd.Stderr = os.Stderr
	err := bag.Cmd.Start()
	if err != nil {
		bag.logger.Printf("Start ffmpeg err:%v", err)
	}
	go func() {
		//防止转码进程自己因为错误停止，出现僵尸进程
		if err2 := bag.Cmd.Wait(); err2 != nil {
			bag.logger.Printf("exit  process error:%v", err2)
		}
		if !bag.PusherTerminated {
			if pusher := GetServer().GetPusher(bag.pusherPath); pusher != nil && pusher.ID() == bag.sessionId && bag.errorTime < bag.MaxRepeatTime {
				//错误重试
				time.Sleep(time.Duration(2) * time.Second)
				bag.errorTime++
				bag.Run(overErrorTimeCallBack)
			} else {
				//超过错误上限，调用回调函数
				overErrorTimeCallBack()
			}
		}
	}()
}

func (bag CmdRepeatBag) PushOver4Kill() {
	defer func() {
		if err := recover(); err != nil {
			bag.logger.Printf("kill  process error:%v", err)
		}
	}()
	bag.PusherTerminated = true
	if bag.Cmd != nil && !bag.Cmd.ProcessState.Exited() {
		if err := bag.Cmd.Process.Kill(); err != nil {
			bag.logger.Printf("kill  process error:%v", err)
		}
	}
}
