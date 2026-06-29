package main

import (
	"flag"
	"io"
	"log"
	"net"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9000", "TCP address to listen on")
	flag.Parse()

	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	log.Printf("echo server listening on %s", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept failed: %v", err)
			continue
		}
		go func() {
			defer conn.Close()
			_, _ = io.Copy(conn, conn)
		}()
	}
}
