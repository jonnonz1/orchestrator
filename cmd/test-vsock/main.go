package main

import (
	"fmt"
	"os"

	"github.com/jonnonz1/orchestrator/internal/agent"
	"github.com/jonnonz1/orchestrator/internal/vsock"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: test-vsock <jail-id> [command...]")
		os.Exit(1)
	}

	jailID := os.Args[1]

	// Ping
	fmt.Printf("Pinging agent in %s...\n", jailID)
	info, err := vsock.Ping(jailID)
	if err != nil {
		fmt.Printf("Ping failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Agent responded: version=%s uptime=%s\n", info.Version, info.Uptime)

	// If command provided, exec it
	if len(os.Args) > 2 {
		cmd := os.Args[2:]
		fmt.Printf("Exec: %v\n", cmd)
		result, err := vsock.ExecStream(jailID, cmd, nil, "/root", func(event agent.StreamEvent) {
			fmt.Printf("[%s] %s\n", event.Type, event.Data)
		})
		if err != nil {
			fmt.Printf("Exec failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exit code: %d\n", result.ExitCode)
	}
}
