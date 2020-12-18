package rtsp

import (
	"net"
	"time"
)

type RichConn struct {
	net.Conn
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (conn *RichConn) Read(b []byte) (n int, err error) {
	if conn.ReadTimeout > 0 {
		_ = conn.Conn.SetReadDeadline(time.Now().Add(conn.ReadTimeout))
	} else {
		var timeout time.Time
		_ = conn.Conn.SetReadDeadline(timeout)
	}
	return conn.Conn.Read(b)
}

func (conn *RichConn) Write(b []byte) (n int, err error) {
	if conn.WriteTimeout > 0 {
		_ = conn.Conn.SetWriteDeadline(time.Now().Add(conn.WriteTimeout))
	} else {
		var timeout time.Time
		_ = conn.Conn.SetWriteDeadline(timeout)
	}
	return conn.Conn.Write(b)
}
