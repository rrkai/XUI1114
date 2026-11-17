package network

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// FlexTLSListener serves TLS and plain HTTP on the same port. When a valid
// panel certificate is available, TLS clients are served and plain HTTP
// clients receive a redirect to HTTPS; when it is missing or expired, plain
// HTTP keeps working so a broken certificate can never lock the operator out.
type FlexTLSListener struct {
	net.Listener
	certs     *CertManager
	tlsConfig *tls.Config

	connCh    chan net.Conn
	errCh     chan error
	done      chan struct{}
	closeOnce sync.Once
}

func NewFlexTLSListener(listener net.Listener, certs *CertManager) net.Listener {
	l := &FlexTLSListener{
		Listener:  listener,
		certs:     certs,
		tlsConfig: &tls.Config{GetCertificate: certs.GetCertificate},
		connCh:    make(chan net.Conn),
		errCh:     make(chan error, 1),
		done:      make(chan struct{}),
	}
	go l.acceptLoop()
	return l
}

// Accept never classifies a connection itself: a client that connects without
// sending anything must not hold up every other pending connection.
func (l *FlexTLSListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case err := <-l.errCh:
		return nil, err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *FlexTLSListener) acceptLoop() {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			select {
			case l.errCh <- err:
			default:
			}
			return
		}
		go l.classify(conn)
	}
}

func (l *FlexTLSListener) classify(conn net.Conn) {
	pc := &bufferedConn{Conn: conn, reader: bufio.NewReader(conn)}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	first, err := pc.reader.Peek(1)
	if err != nil {
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	if first[0] == 0x16 {
		if !l.certs.HasCert() {
			conn.Close()
			return
		}
		l.deliver(tls.Server(pc, l.tlsConfig))
		return
	}

	if l.certs.Valid() {
		go redirectToHTTPS(pc)
		return
	}
	l.deliver(pc)
}

func (l *FlexTLSListener) deliver(conn net.Conn) {
	select {
	case l.connCh <- conn:
	case <-l.done:
		conn.Close()
	}
}

func (l *FlexTLSListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.Listener.Close()
	})
	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func redirectToHTTPS(conn *bufferedConn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	req, err := http.ReadRequest(conn.reader)
	if err != nil {
		return
	}
	uri := req.URL.RequestURI()
	if uri == "" {
		uri = "/"
	}
	location := fmt.Sprintf("https://%s%s", req.Host, uri)
	fmt.Fprintf(conn, "HTTP/1.1 307 Temporary Redirect\r\nLocation: %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", location)
}
