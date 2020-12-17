package rtsp

import (
	"net"
	"time"
)

type RichConn struct {
	net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func (conn *RichConn) Read(b []byte) (n int, err error) {
	if conn.readTimeout > 0 {
		_ = conn.Conn.SetReadDeadline(time.Now().Add(conn.readTimeout))
	} else {
		var timeout time.Time
		_ = conn.Conn.SetReadDeadline(timeout)
	}
	return conn.Conn.Read(b)
}

func (conn *RichConn) Write(b []byte) (n int, err error) {
	if conn.writeTimeout > 0 {
		_ = conn.Conn.SetWriteDeadline(time.Now().Add(conn.writeTimeout))
	} else {
		_ = conn.Conn.SetWriteDeadline(time.Now().Add(time.Duration(10) * time.Second))
	}
	return conn.Conn.Write(b)
}
