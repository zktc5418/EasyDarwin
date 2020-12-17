package rtsp

import (
	"net"
	"time"
)

type RichConn struct {
	net.Conn
	timeout time.Duration
}

func (conn *RichConn) Read(b []byte) (n int, err error) {
	if conn.timeout > 0 {
		_ = conn.Conn.SetReadDeadline(time.Now().Add(conn.timeout))
	} else {
		_ = conn.Conn.SetReadDeadline(time.Now().Add(time.Duration(10) * time.Second))
	}
	return conn.Conn.Read(b)
}

func (conn *RichConn) Write(b []byte) (n int, err error) {
	if conn.timeout > 0 {
		_ = conn.Conn.SetWriteDeadline(time.Now().Add(conn.timeout))
	} else {
		_ = conn.Conn.SetWriteDeadline(time.Now().Add(time.Duration(10) * time.Second))
	}
	return conn.Conn.Write(b)
}
