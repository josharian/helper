package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const addr = ":63419"

var (
	daemon  = flag.Bool("daemon", false, "run as daemon")
	ttl     = flag.Duration("ttl", 5*time.Second, "daemon lifetime without any new connections")
	logfile = flag.String("log", "", "file to log to, when running as daemon, for debugging")
)

// connectToDaemon establishes a connection to a daemon,
// starting one if necessary.
func connectToDaemon() (client *rpc.Client, err error) {
	// Attempt to connect to an existing daemon.
	client, err = rpc.DialHTTP("tcp", addr)
	if err == nil {
		return
	}

	// Failed to connect. Try to start the daemon.
	// We might race with another parent process to start the daemon.
	// That's ok; one of us will win, and the other started daemon
	// will fail, since the port will be in use.
	// Whichever of us fails will attempt to connect, below.
	// That connection attempt races with the daemon's ability
	// to get started in time to accept our connection.
	// If we win, we lose, and just have to go it alone.
	// In practice, I don't expect this to occur
	// much, if ever, so being non-optimal is ok.
	if err := execDaemon(); err != nil {
		log.Printf("daemon failed to start: %v", err)
	}

	return rpc.DialHTTP("tcp", addr)
}

// execDaemon attempts to start a new daemon.
func execDaemon() error {
	lf := *logfile
	if lf != "" {
		abs, err := filepath.Abs(lf)
		if err != nil {
			lf = abs
		}
	}
	cmd := exec.Command(os.Args[0], "-daemon", "-log="+lf, "-ttl="+(*ttl).String())
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}

	// Wait for daemon to be ready (or die)
	buf := bufio.NewReader(pipe)
	line, err := buf.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read from daemon: %v", err)
	}
	if line != "READY\n" {
		return errors.New(line)
	}
	return nil
}

// daemonMain is the daemon's main function.
// rcvr is an RPC-enabled type.
func daemonMain(rcvr interface{}) {
	if *logfile != "" {
		f, err := os.OpenFile(*logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		log.SetOutput(f)
	} else {
		log.SetOutput(ioutil.Discard)
	}

	log.Print("setting up rpc")
	rpcserv := rpc.NewServer()
	err := rpcserv.Register(rcvr)
	if err != nil {
		return
	}

	log.Print("starting listener")
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("failed to listen: %v", err)
		return
	}
	log.Printf("listening on %v", l.Addr())

	log.Print("starting http server")
	activityc := make(chan bool)
	httpserv := http.Server{
		Handler: rpcserv,
		ConnState: func(conn net.Conn, state http.ConnState) {
			log.Printf("conn state (%v): %v", conn.RemoteAddr(), state)
			switch state {
			case http.StateNew, http.StateActive, http.StateIdle:
				activityc <- true
			}
		},
	}

	go httpserv.Serve(l)

	fmt.Println("READY")

	// Loop until ttl elapses with no RPC activity.
	for {
		select {
		case <-activityc:
		case <-time.After(*ttl):
			return
		}
	}
}