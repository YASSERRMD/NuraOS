// Command nuractl is the NuraOS service control CLI.
//
// It communicates with nura-manager over a Unix domain socket
// (/run/nura-manager.sock) using a simple JSON protocol.
//
// Usage:
//
//	nuractl list                    # list all services and their states
//	nuractl status <service>        # detailed status for one service
//	nuractl start  <service>        # request start
//	nuractl stop   <service>        # request stop
//	nuractl restart <service>       # request restart
//	nuractl logs   <service> [-n N] # last N log lines (default 50)
//	nuractl enable  <service>       # mark service enabled
//	nuractl disable <service>       # mark service disabled
//
// Flags:
//
//	--json          emit JSON instead of human-readable output
//	--socket PATH   use a non-default control socket
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
	"github.com/yasserrmd/nuraos/services/internal/diskmon"
)

func main() {
	args := os.Args[1:]
	socketPath := ctlsock.SocketPath
	outputJSON := false

	// Parse global flags.
	remaining := args[:0]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			outputJSON = true
		case "--socket":
			i++
			if i >= len(args) {
				fatalf("--socket requires a path argument")
			}
			socketPath = args[i]
		default:
			remaining = append(remaining, args[i])
		}
	}
	args = remaining

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	client := ctlsock.NewClient(socketPath)
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "list":
		cmdList(client, outputJSON)
	case "status":
		requireArg(cmd, rest)
		cmdStatus(client, rest[0], outputJSON)
	case "start":
		requireArg(cmd, rest)
		cmdStart(client, rest[0], outputJSON)
	case "stop":
		requireArg(cmd, rest)
		cmdStop(client, rest[0], outputJSON)
	case "restart":
		requireArg(cmd, rest)
		cmdRestart(client, rest[0], outputJSON)
	case "logs":
		requireArg(cmd, rest)
		n := 50
		for i := 1; i < len(rest); i++ {
			if rest[i] == "-n" && i+1 < len(rest) {
				v, err := strconv.Atoi(rest[i+1])
				if err != nil {
					fatalf("invalid -n value: %s", rest[i+1])
				}
				n = v
				i++
			}
		}
		cmdLogs(client, rest[0], n, outputJSON)
	case "enable":
		requireArg(cmd, rest)
		cmdEnable(client, rest[0], outputJSON)
	case "disable":
		requireArg(cmd, rest)
		cmdDisable(client, rest[0], outputJSON)
	case "reclaim":
		cmdReclaim(outputJSON)
	default:
		fmt.Fprintf(os.Stderr, "nuractl: unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// --- commands ---

func cmdList(c *ctlsock.Client, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdList}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Printf("%-20s  %-10s  %-8s  %s\n", "NAME", "STATE", "RESTARTS", "SINCE")
	fmt.Println(strings.Repeat("-", 60))
	for _, s := range resp.Services {
		fmt.Printf("%-20s  %-10s  %-8d  %s\n", s.Name, s.State, s.Restarts, s.Since)
	}
}

func cmdStatus(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdStatus, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	s := resp.Service
	if s == nil {
		fatalf("no service data in response")
	}
	fmt.Printf("Name:     %s\n", s.Name)
	fmt.Printf("State:    %s\n", s.State)
	fmt.Printf("PID:      %d\n", s.PID)
	fmt.Printf("Restarts: %d\n", s.Restarts)
	fmt.Printf("Since:    %s\n", s.Since)
	fmt.Printf("Enabled:  %v\n", s.Enabled)
}

func cmdStart(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdStart, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Println(resp.Message)
}

func cmdStop(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdStop, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Println(resp.Message)
}

func cmdRestart(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdRestart, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Println(resp.Message)
}

func cmdLogs(c *ctlsock.Client, svc string, n int, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdLogs, Service: svc, Lines: n}))
	if asJSON {
		printJSON(resp)
		return
	}
	for _, line := range resp.Logs {
		fmt.Println(line)
	}
}

func cmdEnable(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdEnable, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Println(resp.Message)
}

func cmdDisable(c *ctlsock.Client, svc string, asJSON bool) {
	resp := must(c.Send(ctlsock.Request{Command: ctlsock.CmdDisable, Service: svc}))
	if asJSON {
		printJSON(resp)
		return
	}
	fmt.Println(resp.Message)
}

func cmdReclaim(asJSON bool) {
	dataDir := os.Getenv("NURA_DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	freed, err := diskmon.Reclaim(diskmon.ReclaimOptions{
		DataDir:    dataDir,
		SessionCap: 512 * 1024 * 1024,
		LogsCap:    128 * 1024 * 1024,
	})
	if err != nil {
		fatalf("reclaim: %v", err)
	}
	if asJSON {
		printJSON(map[string]interface{}{"freed_bytes": freed, "data_dir": dataDir})
		return
	}
	fmt.Printf("reclaim: freed %d bytes from %s\n", freed, dataDir)
}

// --- helpers ---

func must(resp ctlsock.Response, err error) ctlsock.Response {
	if err != nil {
		fatalf("error communicating with manager: %v", err)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "nuractl: %s\n", resp.Error)
		os.Exit(1)
	}
	return resp
}

func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func requireArg(cmd string, args []string) {
	if len(args) == 0 {
		fatalf("%s: service name required", cmd)
	}
}

func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "nuractl: "+format+"\n", a...)
	os.Exit(1)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: nuractl [--json] [--socket PATH] <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  list                      List all services and states")
	fmt.Fprintln(os.Stderr, "  status  <service>         Show detailed status")
	fmt.Fprintln(os.Stderr, "  start   <service>         Start a service")
	fmt.Fprintln(os.Stderr, "  stop    <service>         Stop a service")
	fmt.Fprintln(os.Stderr, "  restart <service>         Restart a service")
	fmt.Fprintln(os.Stderr, "  logs    <service> [-n N]  Show last N log lines (default 50)")
	fmt.Fprintln(os.Stderr, "  enable  <service>         Mark service enabled")
	fmt.Fprintln(os.Stderr, "  disable <service>         Mark service disabled")
	fmt.Fprintln(os.Stderr, "  reclaim                   Free space by trimming sessions and logs")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --json          JSON output")
	fmt.Fprintln(os.Stderr, "  --socket PATH   Manager socket path (default: /run/nura-manager.sock)")
}
