package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ngoclaw/ngoagent/internal/application"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
)

func main() {
	// Create minimal dependencies
	baseDeps := application.CreateDependencies()
	
	// Create a new session
	sessResponse := baseDeps.AgentAPI.NewSession("Test Session")
	sessionID := sessResponse.SessionID
	fmt.Println("Created session:", sessionID)

	// Send message 1 async
	go func() {
		err := baseDeps.AgentAPI.ChatStream(context.Background(), sessionID, "Write a very long poem about the ocean.", "auto", service.NewBufferedDelta(func(b []byte) bool {
			return true
		}).MakeDelta())
		if err != nil {
			fmt.Println("Msg 1 err:", err)
		}
	}()

	// Wait 2 seconds (while generating) and stop
	time.Sleep(2 * time.Second)
	fmt.Println("Stopping run...")
	baseDeps.AgentAPI.StopRun(sessionID)
	
	// Wait a bit for stop to settle
	time.Sleep(1 * time.Second)

	// Send message 2
	fmt.Println("Sending message 2...")
	err := baseDeps.AgentAPI.ChatStream(context.Background(), sessionID, "What was my previous request?", "auto", service.NewBufferedDelta(func(b []byte) bool {
		return true
	}).MakeDelta())
	if err != nil {
		fmt.Println("Msg 2 err:", err)
	}

	// Wait a bit 
	time.Sleep(1 * time.Second)

	// Fetch history directly from DB
	history, err := baseDeps.AgentAPI.GetHistory(sessionID)
	if err != nil {
		fmt.Println("GetHistory err:", err)
	}
	fmt.Printf("History contains %d messages:\n", len(history))
	for i, msg := range history {
		var content string
		if len(msg.Content) > 30 {
			content = msg.Content[:30] + "..."
		} else {
			content = msg.Content
		}
		fmt.Printf("[%d] %s: %s\n", i, msg.Role, content)
	}
}
