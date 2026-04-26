package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func ServerHandshake(conn net.Conn) error {
	_, err := ServerHandshakeReader(conn)
	return err
}

func ServerHandshakeReader(conn net.Conn) (*bufio.Reader, error) {
	reader := bufio.NewReader(conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(requestLine, "GET ") {
		return nil, fmt.Errorf("unexpected request line: %q", requestLine)
	}

	headers := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}

	key := headers["sec-websocket-key"]
	if key == "" {
		return nil, fmt.Errorf("missing sec-websocket-key")
	}
	accept := computeAccept(key)
	response := strings.Join([]string{
		"HTTP/1.1 101 Switching Protocols",
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Accept: " + accept,
		"",
		"",
	}, "\r\n")
	_, err = conn.Write([]byte(response))
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func ClientHandshake(conn net.Conn, host, path string) error {
	_, err := ClientHandshakeReader(conn, host, path)
	return err
}

func ClientHandshakeReader(conn net.Conn, host, path string) (*bufio.Reader, error) {
	keyBytes := sha1.Sum([]byte(host + path))
	key := base64.StdEncoding.EncodeToString(keyBytes[:16])
	request := strings.Join([]string{
		fmt.Sprintf("GET %s HTTP/1.1", path),
		"Host: " + host,
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Version: 13",
		"Sec-WebSocket-Key: " + key,
		"",
		"",
	}, "\r\n")
	if _, err := conn.Write([]byte(request)); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.Contains(statusLine, "101") {
		return nil, fmt.Errorf("unexpected status line: %q", statusLine)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	return reader, nil
}

func computeAccept(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}
