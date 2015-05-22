package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"sync"
	"time"
)

const addr = ":63419"

var (
	serve   = flag.Bool("serve", false, "start server")
	ttl     = flag.Duration("ttl", 5*time.Second, "time to live without accepting a new connection")
	logfile = flag.String("log", "", "file to log to")
)

func main() {
	flag.Parse()
	if *serve {
		startServer()
		return
	}

	conn := connectToDaemon()
	if conn == nil {
		log.Println("GOING IT ALONE. OMG")
		return
	}

	client, err := rpc.DialHTTP("tcp", addr)
	if err != nil {
		log.Fatal("dialing:", err)
	}

	var reply string
	err = client.Call("Server.Version", struct{}{}, &reply)
	if err != nil {
		log.Fatal("version error:", err)
	}
	fmt.Printf("version: %s\n", reply)

	var n int
	err = client.Call("Server.Requests", struct{}{}, &n)
	if err != nil {
		log.Fatal("reqs error:", err)
	}
	fmt.Printf("n reqs: %d\n", n)

	log.Println("done")
	return
}

type Server struct {
	reqc chan bool  // all requests send on reqc; used for ttl implementation
	mu   sync.Mutex // guards fields below
	n    int        // number of requests
}

func (s *Server) Version(args struct{}, reply *string) error {
	log.Println("version requested")
	s.reqc <- true
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	*reply = "v0"
	return nil
}

func (s *Server) Requests(args struct{}, reply *int) error {
	log.Println("n requests requested")
	s.reqc <- true
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	*reply = s.n
	return nil
}

func connectToDaemon() net.Conn {
	// Attempt to connect to an existing daemon.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		conn = nil
		// Failed to connect. Try to start the daemon.
		// We might race with another parent process
		// to start the child. That's ok; one of us
		// will win, and the other will fail, since
		// the port will be in use. Whichever of us
		// fails will attempt to connect, below.
		// We're now racing the daemon; if we win,
		// then we have to go it alone (i.e. we lose).
		// In practice, I don't expect this to occur,
		// much if ever, so being non-optimal is ok.
		if err := startDaemon(); err != nil {
			log.Printf("failed to start daemon: %v", err)
		}
	}

	// If our first attempt failed, try one more time.
	// We might connect to a newly started daemon,
	// be it by us or by someone else.
	if conn == nil {
		if conn, err = net.Dial("tcp", addr); err != nil {
			log.Printf("second dial failed: %v", err)
		}
	}
	return conn
}

func startDaemon() error {
	cmd := exec.Command(os.Args[0], "-serve", "-log=/Users/jbleechersnyder/go/src/github.com/josharian/helper/log.txt")
	rc, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	buf := bufio.NewReader(rc)
	line, err := buf.ReadString('\n')
	if err != nil {
		return err
	}
	if line != "READY\n" {
		return fmt.Errorf("failed to start daemon, got: %v", line)
	}
	return nil
}

func startServer() {
	if *logfile != "" {
		f, err := os.OpenFile(*logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Println("ERROR", err)
			return
		}
		log.SetOutput(f)
	} else {
		log.SetOutput(ioutil.Discard)
	}

	log.Print("starting server")
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("failed to listen: %v", err)
		fmt.Println("ERROR", err)
		return
	}
	log.Printf("listening on %v", l.Addr())
	fmt.Println("READY")

	s := Server{
		reqc: make(chan bool),
	}
	rpc.Register(&s)
	rpc.HandleHTTP()
	go http.Serve(l, nil)

	for {
		log.Print("waiting for request")
		select {
		case <-s.reqc:
			log.Print("got a request")
		case <-time.After(*ttl):
			log.Print("terminating due to inactivity")
			return
		}
	}
}
