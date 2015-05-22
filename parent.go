package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
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

	buf, err := ioutil.ReadAll(conn)
	log.Printf("recvd=%s err=%v", buf, err)
	log.Println("done")
	return
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
		// fails will just have to go it alone.
		// In practice, I don't expect this to occur,
		// so being non-optimal is ok.
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

type connerr struct {
	net.Conn
	error
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

	c := make(chan connerr)
	for {
		log.Print("waiting for connection")
		go func() {
			conn, err := l.Accept()
			c <- connerr{Conn: conn, error: err}
		}()
		select {
		case <-time.After(*ttl):
			log.Print("terminating due to inactivity")
			return
		case ce := <-c:
			if ce.error != nil {
				log.Printf("could not accept connection: %v", ce.error)
				return
			}
			go handle(ce.Conn)
		}
	}
	time.Sleep(5 * time.Second)
}

var (
	nmu sync.Mutex
	n   int
)

func handle(conn net.Conn) {
	log.Printf("handling connection from %v", conn.RemoteAddr())
	nmu.Lock()
	defer nmu.Unlock()
	n++
	fmt.Fprintf(conn, "connection: %d", n)
	conn.Close()
	log.Printf("done with connection from %v", conn.RemoteAddr())
}
