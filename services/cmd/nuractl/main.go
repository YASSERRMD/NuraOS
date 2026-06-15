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
//	nuractl poweroff                # graceful shutdown then power off
//	nuractl reboot                  # graceful shutdown then reboot
//	nuractl events                  # tail system events from the event bus
//	nuractl pkg install <file>      # install a .nupkg package
//	nuractl pkg list                # list installed packages
//	nuractl pkg remove  <name>      # remove an installed package
//
// Flags:
//
//	--json          emit JSON instead of human-readable output
//	--socket PATH   use a non-default control socket
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
	"github.com/yasserrmd/nuraos/services/internal/delta"
	"github.com/yasserrmd/nuraos/services/internal/diskmon"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
	"github.com/yasserrmd/nuraos/services/internal/history"
	"github.com/yasserrmd/nuraos/services/internal/pkgmgr"
	"github.com/yasserrmd/nuraos/services/internal/update"
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
	case "history":
		cmdHistory(rest, outputJSON)
	case "update":
		cmdUpdate(rest, outputJSON)
	case "pkg":
		cmdPkg(rest, outputJSON)
	case "events":
		cmdEvents(outputJSON)
	case "poweroff":
		cmdShutdown(client, outputJSON, false)
	case "reboot":
		cmdShutdown(client, outputJSON, true)
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

func cmdEvents(asJSON bool) {
	conn, err := net.Dial("unix", eventbus.SocketPath)
	if err != nil {
		fatalf("connect to event bus: %v (is nura-manager running?)", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, `{"subscribe":true}`+"\n"); err != nil {
		fatalf("subscribe: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if asJSON {
			fmt.Println(line)
			continue
		}
		var ev eventbus.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			fmt.Println(line)
			continue
		}
		payload := ""
		if ev.Payload != nil {
			b, _ := json.Marshal(ev.Payload)
			payload = " " + string(b)
		}
		fmt.Printf("%s  %-24s  %s%s\n", ev.At, ev.Type, ev.Source, payload)
	}
	if err := scanner.Err(); err != nil {
		fatalf("event stream: %v", err)
	}
}

func cmdShutdown(c *ctlsock.Client, asJSON bool, reboot bool) {
	cmd := ctlsock.CmdPoweroff
	label := "poweroff"
	if reboot {
		cmd = ctlsock.CmdReboot
		label = "reboot"
	}
	resp := must(c.Send(ctlsock.Request{Command: cmd}))
	if asJSON {
		printJSON(resp)
		return
	}
	if resp.Message != "" {
		fmt.Println(resp.Message)
	} else {
		fmt.Printf("%s initiated\n", label)
	}
}

func cmdHistory(args []string, asJSON bool) {
	dataDir := os.Getenv("NURA_DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	store := history.NewStore(dataDir)

	if len(args) == 0 {
		args = []string{"list"} // default subcommand
	}
	switch args[0] {
	case "list":
		entries, err := store.List()
		if err != nil {
			fatalf("history list: %v", err)
		}
		if asJSON {
			printJSON(entries)
			return
		}
		if len(entries) == 0 {
			fmt.Println("no version history recorded")
			return
		}
		fmt.Printf("%-20s  %-8s  %-8s  %-10s  %s\n", "ID", "SLOT", "KNOWN-GOOD", "VERSION", "TIMESTAMP")
		fmt.Println(strings.Repeat("-", 72))
		for _, e := range entries {
			kg := ""
			if e.KnownGood {
				kg = "yes"
			}
			fmt.Printf("%-20s  %-8s  %-10s  %-10s  %s\n",
				e.ID, e.Slot, kg, e.ImageVersion, e.Timestamp)
		}

	case "mark-good":
		if len(args) < 2 {
			fatalf("history mark-good: entry ID required")
		}
		if err := store.MarkKnownGood(args[1]); err != nil {
			fatalf("history mark-good: %v", err)
		}
		if !asJSON {
			fmt.Printf("entry %s marked as known-good\n", args[1])
		}

	case "rollback":
		if len(args) < 2 {
			fatalf("history rollback: entry ID required")
		}
		if err := store.RollbackTo(args[1], dataDir); err != nil {
			fatalf("history rollback: %v", err)
		}
		if asJSON {
			printJSON(map[string]string{"rolled_back_to": args[1]})
			return
		}
		e, _ := store.Get(args[1])
		fmt.Printf("rolled back to entry %s (slot %s); reboot to activate\n", args[1], e.Slot)

	case "prune":
		max := 10
		if len(args) >= 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				fatalf("history prune: invalid max count %q", args[1])
			}
			max = n
		}
		if err := store.Prune(max); err != nil {
			fatalf("history prune: %v", err)
		}
		if !asJSON {
			fmt.Printf("history pruned to %d entries\n", max)
		}

	default:
		fatalf("history: unknown subcommand %q (list|mark-good|rollback|prune)", args[0])
	}
}

func cmdUpdate(args []string, asJSON bool) {
	dataDir := os.Getenv("NURA_DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	rootfsDir := os.Getenv("NURA_ROOTFS_DIR")
	if rootfsDir == "" {
		rootfsDir = "/boot"
	}
	opts := update.Options{DataDir: dataDir, RootfsDir: rootfsDir}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nuractl update: subcommand required (apply|rollback|abort|log)")
		os.Exit(1)
	}
	switch args[0] {
	case "apply":
		if len(args) < 2 {
			fatalf("update apply: local image file required")
		}
		imgPath := args[1]
		expectedSHA := ""
		for i := 2; i < len(args)-1; i++ {
			if args[i] == "--sha256" {
				expectedSHA = args[i+1]
			}
		}
		f, err := os.Open(imgPath)
		if err != nil {
			fatalf("update apply: open image: %v", err)
		}
		defer f.Close()
		tx, err := update.Apply(f, imgPath, expectedSHA, nil, nil, opts)
		if err != nil {
			fatalf("update apply: %v", err)
		}
		store := history.NewStore(dataDir)
		_ = store.Add(history.Entry{
			ID:       tx.ID,
			Slot:     tx.TargetSlot,
			ImageSHA: tx.ExpectedSHA,
			Source:   tx.Source,
			TxID:     tx.ID,
		}, 0)
		if asJSON {
			printJSON(tx)
			return
		}
		fmt.Printf("update committed: slot %s is now active\n", tx.TargetSlot)

	case "rollback":
		prev, err := update.RollbackLastUpdate(opts)
		if err != nil {
			fatalf("update rollback: %v", err)
		}
		if asJSON {
			printJSON(map[string]string{"rolled_back_to": prev})
			return
		}
		fmt.Printf("rolled back to slot %s\n", prev)

	case "abort":
		if err := update.Abort(opts); err != nil {
			fatalf("update abort: %v", err)
		}
		if !asJSON {
			fmt.Println("update transaction aborted")
		}

	case "log":
		alog := update.NewAuditLog(dataDir)
		entries, err := alog.Entries()
		if err != nil {
			fatalf("update log: %v", err)
		}
		if asJSON {
			printJSON(entries)
			return
		}
		for _, e := range entries {
			fmt.Printf("%s  %-20s  %s  %s\n", e.Timestamp, e.Event, e.TxID, e.Detail)
		}

	case "delta-generate":
		if len(args) < 4 {
			fatalf("update delta-generate <old-image> <new-image> <out.nudelta>")
		}
		oldData, err := os.ReadFile(args[1])
		if err != nil {
			fatalf("read old image: %v", err)
		}
		newData, err := os.ReadFile(args[2])
		if err != nil {
			fatalf("read new image: %v", err)
		}
		out, err := os.Create(args[3])
		if err != nil {
			fatalf("create delta file: %v", err)
		}
		defer out.Close()
		stats, err := delta.Generate(out, oldData, newData, 0)
		if err != nil {
			fatalf("generate delta: %v", err)
		}
		if asJSON {
			printJSON(stats)
			return
		}
		fmt.Printf("delta generated: %d copied, %d new blocks, %.1f%% bandwidth saving\n",
			stats.CopiedBlocks, stats.NewBlocks, stats.SavingsPct())

	case "delta-apply":
		if len(args) < 3 {
			fatalf("update delta-apply <delta.nudelta> <src-slot-image> [--fallback full-image]")
		}
		deltaPath := args[1]
		srcSlotPath := args[2]
		fallbackPath := ""
		for i := 3; i < len(args)-1; i++ {
			if args[i] == "--fallback" {
				fallbackPath = args[i+1]
			}
		}
		df, err := os.Open(deltaPath)
		if err != nil {
			fatalf("open delta file: %v", err)
		}
		defer df.Close()
		var fallbackReader *os.File
		if fallbackPath != "" {
			fallbackReader, err = os.Open(fallbackPath)
			if err != nil {
				fatalf("open fallback image: %v", err)
			}
			defer fallbackReader.Close()
		}
		tx, err := update.ApplyDelta(df, fallbackReader, srcSlotPath, deltaPath, "", nil, nil, opts)
		if err != nil {
			fatalf("delta-apply: %v", err)
		}
		if asJSON {
			printJSON(tx)
			return
		}
		fmt.Printf("delta applied: slot %s is now active\n", tx.TargetSlot)

	default:
		fatalf("update: unknown subcommand %q (apply|rollback|abort|log|delta-generate|delta-apply)", args[0])
	}
}

func cmdPkg(args []string, asJSON bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nuractl pkg: subcommand required (install|list|remove)")
		os.Exit(1)
	}

	pubKeyPath := os.Getenv("NURA_PKG_PUBKEY")
	if pubKeyPath == "" {
		pubKeyPath = pkgmgr.DefaultPubKeyPath
	}
	var opts pkgmgr.Options
	opts.DBPath = os.Getenv("NURA_PKG_DB")
	opts.OverlayDir = os.Getenv("NURA_PKG_OVERLAY")

	switch args[0] {
	case "install":
		if len(args) < 2 {
			fatalf("pkg install: package file required")
		}
		pub, err := pkgmgr.LoadPubKey(pubKeyPath)
		if err != nil {
			fatalf("pkg install: load public key from %s: %v", pubKeyPath, err)
		}
		opts.PubKey = pub
		m, err := pkgmgr.Install(args[1], opts)
		if err != nil {
			fatalf("pkg install: %v", err)
		}
		if asJSON {
			printJSON(map[string]string{"name": m.Name, "version": m.Version, "status": "installed"})
			return
		}
		fmt.Printf("installed %s-%s\n", m.Name, m.Version)

	case "list":
		recs, err := pkgmgr.List(opts)
		if err != nil {
			fatalf("pkg list: %v", err)
		}
		if asJSON {
			printJSON(recs)
			return
		}
		if len(recs) == 0 {
			fmt.Println("no packages installed")
			return
		}
		fmt.Printf("%-24s  %-12s  %s\n", "NAME", "VERSION", "INSTALLED")
		fmt.Println(strings.Repeat("-", 60))
		for _, r := range recs {
			fmt.Printf("%-24s  %-12s  %s\n", r.Name, r.Version, r.InstalledAt)
		}

	case "remove":
		if len(args) < 2 {
			fatalf("pkg remove: package name required")
		}
		if err := pkgmgr.Remove(args[1], opts); err != nil {
			fatalf("pkg remove: %v", err)
		}
		if asJSON {
			printJSON(map[string]string{"name": args[1], "status": "removed"})
			return
		}
		fmt.Printf("removed %s\n", args[1])

	default:
		fatalf("pkg: unknown subcommand %q (install|list|remove)", args[0])
	}
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
	fmt.Fprintln(os.Stderr, "  poweroff                  Graceful shutdown then power off")
	fmt.Fprintln(os.Stderr, "  reboot                    Graceful shutdown then reboot")
	fmt.Fprintln(os.Stderr, "  events                    Tail system events from the event bus")
	fmt.Fprintln(os.Stderr, "  pkg install <file.nupkg>  Install a signed package")
	fmt.Fprintln(os.Stderr, "  pkg list                  List installed packages")
	fmt.Fprintln(os.Stderr, "  pkg remove  <name>        Remove an installed package")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --json          JSON output")
	fmt.Fprintln(os.Stderr, "  --socket PATH   Manager socket path (default: /run/nura-manager.sock)")
}
