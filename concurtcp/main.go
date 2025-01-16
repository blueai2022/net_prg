package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	numWorkers = 5
)

// Task implementation for handling a connection
type ConnectionTask struct {
	conn net.Conn
}

func (task *ConnectionTask) Run(wg *sync.WaitGroup) {
	defer func() {
		task.conn.Close()
		wg.Done()
	}()

	// Read data from the client
	data, err := bufio.NewReader(task.conn).ReadString('\n')
	if err != nil {
		log.Printf("error reading from client: %v\n", err)
		return
	}

	// Process the data and generate a response
	response := fmt.Sprintf("Received: %s", data)

	// Send the response back to the client
	_, err = task.conn.Write([]byte(response))
	if err != nil {
		log.Printf("error writing to client: %v\n", err)
		return
	}
}

func main() {
	if len(os.Args) == 1 {
		log.Fatal("please provide host:port")
	}

	tcpAdr, err := net.ResolveTCPAddr("tcp4", os.Args[1])
	if err != nil {
		log.Fatal("cannot resolve address", os.Args[1])
	}

	listener, err := net.ListenTCP("tcp", tcpAdr)
	if err != nil {
		log.Fatal("cannot listen on address", tcpAdr.String())
	}
	log.Println("TCP server started listening on", tcpAdr.String())

	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a channel to receive interrupt signals
	chSig := make(chan os.Signal, 1)
	signal.Notify(chSig, os.Interrupt, syscall.SIGTERM)

	// Goroutine to handle the interrupt signal
	go func() {
		<-chSig
		fmt.Println("Interrupt signal received. Cancelling context...")
		cancel()
		listener.Close()
	}()

	// Create a worker pool with a fixed number of workers
	pool := NewPool(numWorkers)
	pool.Run()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down server...")

			// Close the pool and wait for all tasks to complete
			pool.Close()
			pool.Wait()

			log.Println("Server shutdown complete.")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				log.Println("cannot accept connection on listener", err)
				continue
			}

			// Create a new task for each connection and add it to the pool
			task := &ConnectionTask{conn: conn}
			pool.Submit(task)
		}
	}
}
