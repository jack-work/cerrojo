package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jack-work/cerrojo/internal/bar"
	"github.com/jack-work/cerrojo/internal/daemon"
	"github.com/jack-work/cerrojo/internal/ipc"
	"github.com/jack-work/cerrojo/internal/step"
)

const usage = `cerrojo locks the screen after a stretch of idleness you can cycle.

usage:
  cerrojo run [flags]      the daemon; see cerrojo run -h
  cerrojo next             move to the next step
  cerrojo prev             move to the previous step
  cerrojo set STEP         jump to a step, e.g. 30m or never
  cerrojo status [--json]  the current step and the countdown
  cerrojo waybar           JSON lines for a waybar custom module
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "run":
		err = run(ctx, args)
	case "next", "prev":
		err = move(cmd, args)
	case "set":
		if len(args) != 1 {
			err = fmt.Errorf("usage: cerrojo set STEP")
			break
		}
		err = move("set "+args[0], nil)
	case "status":
		err = status(args)
	case "waybar":
		err = bar.Stream(ctx, os.Stdout)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Printf("cerrojo: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	steps := fs.String("steps", "5m,30m,1h,never", "the idle timeouts to cycle through, first is the default")
	grace := fs.Duration("grace", 10*time.Second, "idle this long before the countdown shows")
	lock := fs.String("lock", "swaylock -f", "shell command that locks the screen and returns once it has")
	sleep := fs.Bool("lock-before-sleep", true, "lock when logind announces suspend")
	fs.Parse(args)

	parsed, err := step.ParseList(*steps)
	if err != nil {
		return err
	}
	if *grace < 0 {
		return fmt.Errorf("grace %s is negative", *grace)
	}
	return daemon.Run(ctx, daemon.Config{
		Steps:           parsed,
		Grace:           *grace,
		LockCommand:     *lock,
		LockBeforeSleep: *sleep,
	})
}

func move(cmd string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s takes no arguments", cmd)
	}
	s, err := ipc.Ask(cmd)
	if err != nil {
		return err
	}
	fmt.Println(s.Step)
	return nil
}

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the daemon's state as JSON")
	fs.Parse(args)

	s, err := ipc.Ask("status")
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(s)
	}
	fmt.Println(bar.Describe(&s, time.Now()))
	return nil
}
