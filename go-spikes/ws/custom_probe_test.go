package ws

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCustomWebSocketProbeCanReadAndMutateFrames(t *testing.T) {
	clientSide, proxyClientSide := net.Pipe()
	proxyServerSide, serverSide := net.Pipe()

	errCh := make(chan error, 3)

	go func() {
		defer clientSide.Close()
		clientReader, err := ClientHandshakeReader(clientSide, "example.test", "/backend-api/codex/responses")
		if err != nil {
			errCh <- fmt.Errorf("client handshake: %w", err)
			return
		}
		payload := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/prod")
		if err := WriteFrame(clientSide, payload, opcodeText, true); err != nil {
			errCh <- fmt.Errorf("client write frame: %w", err)
			return
		}
		frame, err := ReadFrame(clientReader)
		if err != nil {
			errCh <- fmt.Errorf("client read response frame: %w", err)
			return
		}
		if got := string(frame.Payload); got != "MASKED_OK" {
			errCh <- errUnexpectedPayload(got)
			return
		}
		errCh <- nil
	}()

	go func() {
		defer proxyClientSide.Close()
		defer proxyServerSide.Close()

		clientReader, err := ServerHandshakeReader(proxyClientSide)
		if err != nil {
			errCh <- fmt.Errorf("proxy server handshake: %w", err)
			return
		}
		if _, err := ClientHandshakeReader(proxyServerSide, "example.test", "/backend-api/codex/responses"); err != nil {
			errCh <- fmt.Errorf("proxy client handshake: %w", err)
			return
		}

		frame, err := ReadFrame(clientReader)
		if err != nil {
			errCh <- fmt.Errorf("proxy read client frame: %w", err)
			return
		}
		mutated := strings.ReplaceAll(string(frame.Payload), "postgres://user:pass@localhost:5432/prod", "post************************prod[GENERIC_CONNECTION_STRING:deadbeef]")
		if err := WriteFrame(proxyServerSide, []byte(mutated), opcodeText, true); err != nil {
			errCh <- fmt.Errorf("proxy write upstream frame: %w", err)
			return
		}

		if err := WriteFrame(proxyClientSide, []byte("MASKED_OK"), opcodeText, false); err != nil {
			errCh <- fmt.Errorf("proxy write client frame: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		defer serverSide.Close()
		serverReader, err := ServerHandshakeReader(serverSide)
		if err != nil {
			errCh <- fmt.Errorf("server handshake: %w", err)
			return
		}
		frame, err := ReadFrame(serverReader)
		if err != nil {
			errCh <- fmt.Errorf("server read frame: %w", err)
			return
		}
		if !strings.Contains(string(frame.Payload), "[GENERIC_CONNECTION_STRING:deadbeef]") {
			errCh <- errUnexpectedPayload(string(frame.Payload))
			return
		}
		errCh <- nil
	}()

	timeout := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatal(err)
			}
		case <-timeout:
			t.Fatal("timed out waiting for websocket probe")
		}
	}
}

func errUnexpectedPayload(payload string) error {
	return &payloadError{payload: payload}
}

type payloadError struct {
	payload string
}

func (e *payloadError) Error() string {
	return "unexpected payload: " + e.payload
}
