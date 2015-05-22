package main

import (
	"log"
	"sync"
)

type Server struct {
	mu sync.Mutex // guards fields below
	n  int        // number of requests
}

func (s *Server) Version(args struct{}, reply *string) error {
	log.Println("version requested")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	*reply = "v0"
	return nil
}

func (s *Server) Requests(args struct{}, reply *int) error {
	log.Println("n requests requested")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	*reply = s.n
	return nil
}
