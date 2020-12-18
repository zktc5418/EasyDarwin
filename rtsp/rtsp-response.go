package rtsp

import (
	"fmt"
	"strconv"
	"strings"
)

type Response struct {
	Version    string
	StatusCode int
	Status     string
	Header     map[string]interface{}
	Body       string
	RawBody    []byte
}

func NewResponse(statusCode int, status, cSeq, sid, body string) *Response {
	res := &Response{
		Version:    RTSP_VERSION,
		StatusCode: statusCode,
		Status:     status,
		Header:     map[string]interface{}{"CSeq": cSeq, "Session": sid},
		Body:       body,
	}
	res.SetBody(body)
	return res
}

func (r *Response) String() string {
	str := fmt.Sprintf("%s %d %s\r\n", r.Version, r.StatusCode, r.Status)
	for key, value := range r.Header {
		str += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	str += "\r\n"
	str += r.Body
	return str
}

func (r *Response) ByteData() []byte {
	str := fmt.Sprintf("%s %d %s\r\n", r.Version, r.StatusCode, r.Status)
	for key, value := range r.Header {
		str += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	str += "\r\n"
	return append([]byte(str), r.RawBody...)
}

func (r *Response) SetBody(body string) {
	if body != "" && !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}
	r.RawBody = []byte(body)
	ln := len(r.RawBody)
	if ln > 0 {
		r.Header["Content-Length"] = strconv.Itoa(ln)
	} else {
		delete(r.Header, "Content-Length")
	}
}
