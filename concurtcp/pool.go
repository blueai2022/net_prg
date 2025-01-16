package main

import (
	"sync"
)

type Task interface {
	Run(*sync.WaitGroup)
}

type Pool struct {
	// tasks      []Task
	numThreads int
	tasksChan  chan Task
	wg         sync.WaitGroup
}

func NewPool(numThreads int) *Pool {
	return &Pool{
		numThreads: numThreads,
		tasksChan:  make(chan Task),
	}
}

func (pool *Pool) Run() {
	// start pool number of threads to wait for task on channel
	for i := 0; i < pool.numThreads; i++ {
		go pool.runWorker()
	}
}

func (pool *Pool) runWorker() {
	for task := range pool.tasksChan {
		task.Run(&pool.wg)
	}
}

// Wait waits for all submitted tasks to complete
func (pool *Pool) Wait() {
	pool.wg.Wait()
}

// Close closes the task channel, indicating no more tasks will be submitted
func (pool *Pool) Close() {
	close(pool.tasksChan)
}

// Submit adds a new task to the pool
func (pool *Pool) Submit(task Task) {
	pool.wg.Add(1)
	pool.tasksChan <- task
}
