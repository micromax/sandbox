package proxy

import (
	"net"
	"sync"
	"time"
)

func (p *Proxy) startTCP() {
	go func() {
		for {
			clientConn, err := p.listener.Accept()
			if err != nil {
				select {
				case <-p.closed:
					return
				default:
					return
				}
			}

			go p.forwardTCP(clientConn)
		}
	}()
}

func (p *Proxy) forwardTCP(clientConn net.Conn) {
	defer clientConn.Close()

	targetConn, err := net.DialTimeout("tcp", p.cfg.Target, 5*time.Second)
	if err != nil {
		return
	}
	defer targetConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			if p.cfg.Limits.IdleTimeout > 0 {
				_ = src.SetReadDeadline(time.Now().Add(p.cfg.Limits.IdleTimeout))
				_ = dst.SetWriteDeadline(time.Now().Add(p.cfg.Limits.IdleTimeout))
			}
			nr, rerr := src.Read(buf)
			if nr > 0 {
				nw, werr := dst.Write(buf[0:nr])
				if werr != nil {
					break
				}
				if nr != nw {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		// Close write half if supported
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}

	go pipe(targetConn, clientConn)
	go pipe(clientConn, targetConn)

	wg.Wait()
}
