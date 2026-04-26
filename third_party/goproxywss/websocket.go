package goproxy

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

func headerContains(header http.Header, name string, value string) bool {
	for _, v := range header[name] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(value, strings.TrimSpace(s)) {
				return true
			}
		}
	}
	return false
}

func isWebSocketHandshake(header http.Header) bool {
	return headerContains(header, "Connection", "Upgrade") &&
		headerContains(header, "Upgrade", "websocket")
}

func (proxy *ProxyHttpServer) hijackConnection(ctx *ProxyCtx, w http.ResponseWriter) (net.Conn, error) {
	// Connect to Client
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("httpserver does not support hijacking")
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		ctx.Warnf("Hijack error: %v", err)
		return nil, err
	}
	return clientConn, nil
}

func (proxy *ProxyHttpServer) proxyWebsocket(ctx *ProxyCtx, remoteConn io.ReadWriter, proxyClient io.ReadWriter) {
	if !proxy.shouldInterceptWebSocket(ctx) {
		proxy.proxyWebsocketRaw(ctx, remoteConn, proxyClient)
		return
	}
	proxy.proxyWebsocketFramed(ctx, remoteConn, proxyClient)
}

func (proxy *ProxyHttpServer) shouldInterceptWebSocket(ctx *ProxyCtx) bool {
	if proxy.WebSocketMessageHandler == nil {
		return false
	}
	if proxy.WebSocketMessagePredicate == nil {
		return true
	}
	return proxy.WebSocketMessagePredicate(ctx)
}

func (proxy *ProxyHttpServer) proxyWebsocketRaw(ctx *ProxyCtx, remoteConn io.ReadWriter, proxyClient io.ReadWriter) {
	// 2 is the number of goroutines, this code is implemented according to
	// https://stackoverflow.com/questions/52031332/wait-for-one-goroutine-to-finish
	waitChan := make(chan struct{}, 2)
	go func() {
		_ = copyOrWarn(ctx, remoteConn, proxyClient)
		waitChan <- struct{}{}
	}()

	go func() {
		_ = copyOrWarn(ctx, proxyClient, remoteConn)
		waitChan <- struct{}{}
	}()

	// Wait until one end closes the connection
	<-waitChan
}

func (proxy *ProxyHttpServer) proxyWebsocketFramed(ctx *ProxyCtx, remoteConn io.ReadWriter, proxyClient io.ReadWriter) {
	waitChan := make(chan struct{}, 2)
	go func() {
		_ = proxy.copyWebSocketFrames(ctx, remoteConn, proxyClient, WebSocketClientToServer)
		waitChan <- struct{}{}
	}()

	go func() {
		_ = proxy.copyWebSocketFrames(ctx, proxyClient, remoteConn, WebSocketServerToClient)
		waitChan <- struct{}{}
	}()

	<-waitChan
}

func (proxy *ProxyHttpServer) copyWebSocketFrames(ctx *ProxyCtx, dst io.Writer, src io.Reader, direction WebSocketDirection) error {
	reader := bufio.NewReader(src)
	for {
		frame, masked, err := readWebSocketFrame(reader)
		if err != nil {
			return err
		}

		if proxy.WebSocketMessageHandler != nil {
			frame, err = proxy.WebSocketMessageHandler(ctx, direction, frame)
			if err != nil {
				return err
			}
		}

		if err := writeWebSocketFrame(dst, frame, masked); err != nil {
			return err
		}
	}
}

func readWebSocketFrame(r *bufio.Reader) (WebSocketFrame, bool, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return WebSocketFrame{}, false, err
	}

	frame := WebSocketFrame{
		FIN:    header[0]&0x80 != 0,
		Opcode: header[0] & 0x0f,
	}
	masked := header[1]&0x80 != 0
	payloadLen := uint64(header[1] & 0x7f)
	switch payloadLen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return WebSocketFrame{}, false, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return WebSocketFrame{}, false, err
		}
		payloadLen = binary.BigEndian.Uint64(ext[:])
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return WebSocketFrame{}, false, err
		}
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return WebSocketFrame{}, false, err
	}
	if masked {
		applyWebSocketMask(payload, maskKey)
	}
	frame.Payload = payload
	return frame, masked, nil
}

func writeWebSocketFrame(w io.Writer, frame WebSocketFrame, masked bool) error {
	first := frame.Opcode
	if frame.FIN {
		first |= 0x80
	}

	payloadLen := len(frame.Payload)
	second := byte(0)
	if masked {
		second |= 0x80
	}

	var header []byte
	switch {
	case payloadLen <= 125:
		header = []byte{first, second | byte(payloadLen)}
	case payloadLen <= 65535:
		header = make([]byte, 4)
		header[0] = first
		header[1] = second | 126
		binary.BigEndian.PutUint16(header[2:], uint16(payloadLen))
	default:
		header = make([]byte, 10)
		header[0] = first
		header[1] = second | 127
		binary.BigEndian.PutUint64(header[2:], uint64(payloadLen))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}

	body := append([]byte(nil), frame.Payload...)
	if masked {
		maskKey := [4]byte{0x11, 0x22, 0x33, 0x44}
		if _, err := w.Write(maskKey[:]); err != nil {
			return err
		}
		applyWebSocketMask(body, maskKey)
	}
	_, err := w.Write(body)
	return err
}

func applyWebSocketMask(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}

func copyWebSocketFrameOnce(dst io.Writer, src io.Reader, handler WebSocketMessageHandler) error {
	reader := bufio.NewReader(src)
	frame, masked, err := readWebSocketFrame(reader)
	if err != nil {
		return err
	}
	if handler != nil {
		frame, err = handler(nil, WebSocketClientToServer, frame)
		if err != nil {
			return err
		}
	}
	if err := writeWebSocketFrame(dst, frame, masked); err != nil {
		return err
	}
	return nil
}

func formatWebSocketCopyError(direction WebSocketDirection, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("websocket copy %s: %w", direction, err)
}
