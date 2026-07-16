package main

import (
	"fmt"
	"net/http"

	"github.com/muthuishere/agent-proxy/internal/config"
	agentruntime "github.com/muthuishere/agent-proxy/internal/runtime"
)

func main() {
	cfg, _ := config.Load("/Users/muthuishere/muthu/gitworkspace/agent-proxy-workspace/agent-proxy/config/agentproxy.yaml")
	cfg.Logging.LogFile = "/tmp/debug.jsonl"
	svc, _ := agentruntime.New(cfg)
	defer svc.Close()

	// Try with a detectable secret in a quoted assignment context
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	frame := `{"content":"ANTHROPIC_API_KEY=\"` + secret + `\""}`
	masked, count := svc.HandleWebSocket("debug5", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	fmt.Printf("WS quoted api key count=%d masked=%s\n", count, masked)

	// Combine quoted env assignment with a detectable secret value
	frame2 := `{"content":"export API_KEY=\"` + secret + `\""}`
	_, count2, _ := svc.HandleRequest("debug6", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(frame2), "")
	fmt.Printf("HandleRequest quoted api key count=%d\n", count2)

	// And test the \[ scenario with an API key in quoted env assignment
	frame3 := `{"content":"password=\"supersecretpassword123\"` + secret + `"}`
	masked3, count3 := svc.HandleWebSocket("debug7", "api.anthropic.com", "/backend-api/codex/responses", frame3, true, "")
	fmt.Printf("WS combined count=%d masked=%s\n", count3, masked3)
}
