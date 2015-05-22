package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	flag.Parse()
	if *daemon {
		s := new(Server)
		daemonMain(s)
		return
	}

	client, err := connectToDaemon()
	if err != nil {
		log.Fatal(err) // normally we should plan to be able to go it alone (?)
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
