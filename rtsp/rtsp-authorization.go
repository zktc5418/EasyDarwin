package rtsp

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/bruce-qin/EasyDarwin/models"
	"github.com/bruce-qin/EasyGoLib/db"
	"io/ioutil"
	"net/http"
	"regexp"
)

type AuthorizationType string

const (
	BASIC  AuthorizationType = "Basic"
	DIGEST AuthorizationType = "Digest"
)

var (
	REALM_REX           *regexp.Regexp = regexp.MustCompile(`realm="(.*?)"`)
	NONCE_REX           *regexp.Regexp = regexp.MustCompile(`nonce="(.*?)"`)
	USERNAME_REX        *regexp.Regexp = regexp.MustCompile(`username="(.*?)"`)
	RESPONSE_REX        *regexp.Regexp = regexp.MustCompile(`response="(.*?)"`)
	URI_REX             *regexp.Regexp = regexp.MustCompile(`uri="(.*?)"`)
	BASIC_REX           *regexp.Regexp = regexp.MustCompile(`Basic(\s+)([\w=]+)`)
	USERNAME_PASSWD_REX *regexp.Regexp = regexp.MustCompile(`[:]`)
)

type AuthorizationInfo struct {
	AuthType      AuthorizationType `json:"authType"`
	Username      string            `json:"username"`
	Password      string            `json:"password"`
	Realm         string            `json:"realm"`
	Nonce         string            `json:"nonce"`
	Uri           string            `json:"uri"`
	Response      string            `json:"response"`
	RequestMethod string            `json:"requestMethod"`
}

type AuthError struct {
	authLine string
	err      string
}

func DecodeAuthorizationInfo(authLine string, serverNonce string, requestMethod string) (authInfo *AuthorizationInfo, err error) {
	server := GetServer()
	authInfo = &AuthorizationInfo{
		AuthType:      server.authorizationType,
		RequestMethod: requestMethod,
	}
	authError := &AuthError{
		authLine: authLine,
	}
	if server.authorizationType == BASIC {
		baseMatch := BASIC_REX.FindStringSubmatch("Basic YWRtaW46YWRtaW4=")
		authByte, decErr := base64.StdEncoding.DecodeString(baseMatch[2])
		if decErr != nil {
			authError.err = decErr.Error()
			return nil, authError
		}
		split := USERNAME_PASSWD_REX.Split(string(authByte), 2)
		if len(split) != 2 {
			authError.err = "username and password check error"
			return nil, authError
		}
		authInfo.Username = split[0]
		authInfo.Password = split[1]
		return authInfo, nil
	} else if server.authorizationType == DIGEST {
		result1 := REALM_REX.FindStringSubmatch(authLine)
		if len(result1) == 2 {
			authInfo.Realm = result1[1]
		} else {
			authError.err = "no realm found"
			return nil, authError
		}
		result1 = NONCE_REX.FindStringSubmatch(authLine)
		if len(result1) == 2 {
			authInfo.Nonce = result1[1]
		} else {
			authError.err = "no nonce found"
			return nil, authError
		}
		if serverNonce != authInfo.Nonce {
			authError.err = "server nonce not same as client nonce"
			return nil, authError
		}

		result1 = USERNAME_REX.FindStringSubmatch(authLine)
		if len(result1) == 2 {
			authInfo.Username = result1[1]
		} else {
			authError.err = "username not found"
			return nil, authError
		}

		result1 = RESPONSE_REX.FindStringSubmatch(authLine)
		if len(result1) == 2 {
			authInfo.Response = result1[1]
		} else {
			authError.err = "response not found"
			return nil, authError
		}

		result1 = URI_REX.FindStringSubmatch(authLine)
		if len(result1) == 2 {
			authInfo.Uri = result1[1]
		} else {
			authError.err = "uri not found"
			return nil, authError
		}
		return authInfo, nil
	} else {
		authError.err = string("not support server authorizationType: " + server.authorizationType)
		return nil, authError
	}
}

func (authInfo *AuthorizationInfo) CheckAuthLocal() error {
	var user models.User
	err := db.SQLite.Where("Username = ?", authInfo.Username).First(&user).Error
	if err != nil {
		return fmt.Errorf("CheckAuth error : user not exists")
	}
	//response = MD5(MD5(username:realm:password):nonce:MD5(method:uri))
	md5UserRealmPwd := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s:%s", authInfo.Username, authInfo.Realm, user.Password))))
	md5MethodURL := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s", authInfo.RequestMethod, authInfo.Uri))))
	myResponse := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s:%s", md5UserRealmPwd, authInfo.Nonce, md5MethodURL))))
	if myResponse != authInfo.Response {
		return fmt.Errorf("CheckAuth error : response not equal")
	}
	return nil
}

func (authInfo *AuthorizationInfo) CheckAuthHttpRemote() error {
	authInfoByte, _ := json.Marshal(authInfo)
	//{
	//    "authType": "Digest",
	//    "username": "admin",
	//    "password": "",
	//    "realm": "IP Camera(23435)",
	//    "nonce": "8fd7c44874480bd643d970149224da11",
	//    "uri": "rtsp://192.168.1.76:554/live/123456",
	//    "response": "ca29ba3297f50b32425e46e23723ef7b",
	//    "requestMethod": "Play"
	//}
	response, err := http.Post(GetServer().remoteHttpAuthorizationUrl, "application/json", bytes.NewReader(authInfoByte))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("remote check error :: response return status code:%d", response.StatusCode)
	}
	if result := string(body); result != "0" {
		return fmt.Errorf("remote check error :: response return :%s", result)
	}
	return nil
}

func (err *AuthError) Error() string {
	return fmt.Sprintf("rtsp header[Authorization]%s;check error:%s", err.authLine, err.err)
}
