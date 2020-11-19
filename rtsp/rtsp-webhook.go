package rtsp

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
)

type WebHookActionType string

const (
	ON_PLAY     WebHookActionType = "on_play"
	ON_STOP     WebHookActionType = "on_stop"
	ON_PUBLISH  WebHookActionType = "on_publish"
	ON_TEARDOWN WebHookActionType = "on_teardown"
)

type WebHookInfo struct {
	ID          string            `json:"clientId"`
	SessionType string            `json:"sessionType"`
	TransType   string            `json:"transType"`
	URL         string            `json:"url"`
	Path        string            `json:"path"`
	SDP         string            `json:"sdp"`
	ActionType  WebHookActionType `json:"actionType"`
	ClientAddr  string            `json:"clientAddr"`
}

func NewWebHookInfo(ActionType WebHookActionType, ID string, sessionType SessionType, transType TransType, url, path, sdp, clientAddr string) (webHook *WebHookInfo) {
	webHook = &WebHookInfo{
		ActionType:  ActionType,
		ID:          ID,
		SessionType: sessionType.String(),
		TransType:   transType.String(),
		URL:         url,
		Path:        path,
		SDP:         sdp,
		ClientAddr:  strings.Split(clientAddr, ":")[0],
	}
	return
}

func (session *Session) ToCloseWebHookInfo() (webHook *WebHookInfo) {
	var ActionType WebHookActionType
	if session.Type == SESSEION_TYPE_PLAYER {
		ActionType = ON_STOP
	} else {
		ActionType = ON_TEARDOWN
	}
	return session.ToWebHookInfo(ActionType)
}

func (session *Session) ToWebHookInfo(ActionType WebHookActionType) (webHook *WebHookInfo) {
	webHook = &WebHookInfo{
		ActionType:  ActionType,
		ID:          session.ID,
		SessionType: session.Type.String(),
		TransType:   session.TransType.String(),
		URL:         session.URL,
		Path:        session.Path,
		SDP:         session.SDPRaw,
		ClientAddr:  strings.Split(session.Conn.RemoteAddr().String(), ":")[0],
	}
	return
}

func (webHook WebHookInfo) ExecuteWebHookNotify() (success bool) {
	server := GetServer()
	success = true
	var webHookUrls []string
	switch webHook.ActionType {
	case ON_PLAY:
		webHookUrls = server.onPlay
		success = false
	case ON_STOP:
		webHookUrls = server.onStop
	case ON_PUBLISH:
		webHookUrls = server.onPublish
		success = false
	case ON_TEARDOWN:
		webHookUrls = server.onTeardown
	}
	if len(webHookUrls) == 0 {
		return true
	}
	jsonBytes, _ := json.Marshal(webHook)
	for _, url := range webHookUrls {
		response, err := http.Post(url, "application/json", bytes.NewReader(jsonBytes))
		if err != nil {
			server.logger.Printf("request web hook [%s] error:%v", url, err)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			continue
		} else if response.StatusCode != 200 {
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			continue
		}
		resultBytes, err := ioutil.ReadAll(response.Body)
		if err != nil {
			server.logger.Printf("request web hook [%s] error:%v", url, err)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			continue
		}
		if result := string(resultBytes); result != "0" {
			server.logger.Printf("request web hook [%s] return error:%v", url, result)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			continue
		}
		response.Body.Close()
		return true
	}
	return
}
