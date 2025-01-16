package main

import (
	"bufio"
	"log"
	"net"
	"os"
)

func main() {
	if len(os.Args) == 1 {
		log.Fatal("please provide host:port")
	}

	tcpAdr, err := net.ResolveTCPAddr("tcp4", os.Args[1])
	if err != nil {
		log.Fatal(" ", os.Args[1])
	}

	conn, err := net.DialTCP("tcp", nil, tcpAdr)
	if err != nil {
		log.Fatal(" ", tcpAdr.String())
	}

	_, err = conn.Write([]byte("Hello, server\n"))
	if err != nil {
		log.Fatal(" ", err)
	}

	data, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Fatal(" ", err)
	}
	log.Println("> ", data)
}
