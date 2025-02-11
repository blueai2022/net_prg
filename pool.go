package main

import (
	"sync"
)

type Task interface {
	Run(*sync.WaitGroup)
}

type Pool struct {
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
	for i := 0; i < pool.numThreads; i++ {
		go pool.runWorker()
	}
}

func (pool *Pool) runWorker() {
	for task := range pool.tasksChan {
		task.Run(&pool.wg)
	}
}

func (pool *Pool) Wait() {
	pool.wg.Wait()
}

func (pool *Pool) Close() {
	close(pool.tasksChan)
}

func (pool *Pool) Submit(task Task) {
	pool.wg.Add(1)
	pool.tasksChan <- task
}
