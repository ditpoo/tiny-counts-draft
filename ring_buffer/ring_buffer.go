package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type RingBuffer struct {
	data       []CounterRequest
	head, tail int
	size       int
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]CounterRequest, size),
		size: size,
	}
}

func (rb *RingBuffer) Add(req CounterRequest) {
	rb.data[rb.tail] = req
	rb.tail = (rb.tail + 1) % rb.size
	if rb.tail == rb.head {
		rb.head = (rb.head + 1) % rb.size
	}
}

func (rb *RingBuffer) RetrieveAll() []CounterRequest {
	var result []CounterRequest
	if rb.head < rb.tail {
		result = rb.data[rb.head:rb.tail]
	} else {
		result = append(rb.data[rb.head:], rb.data[:rb.tail]...)
	}
	rb.head, rb.tail = 0, 0
	return result
}

type PartitionedRingBuffer struct {
	buffers   []*RingBuffer
	locks     []*sync.Mutex
	partitions int
}

func NewPartitionedRingBuffer(partitions, size int) *PartitionedRingBuffer {
	buffers := make([]*RingBuffer, partitions)
	locks := make([]*sync.Mutex, partitions)
	for i := 0; i < partitions; i++ {
		buffers[i] = NewRingBuffer(size)
		locks[i] = &sync.Mutex{}
	}
	return &PartitionedRingBuffer{
		buffers:   buffers,
		locks:     locks,
		partitions: partitions,
	}
}

func (prb *PartitionedRingBuffer) Add(req CounterRequest) {
	index := int(req.hashCode()) % prb.partitions
	prb.locks[index].Lock()
	defer prb.locks[index].Unlock()
	prb.buffers[index].Add(req)
}

func (prb *PartitionedRingBuffer) RetrieveAll() []CounterRequest {
	var result []CounterRequest
	for i := 0; i < prb.partitions; i++ {
		prb.locks[i].Lock()
		result = append(result, prb.buffers[i].RetrieveAll()...)
		prb.locks[i].Unlock()
	}
	return result
}

type SharedData struct {
	WebsocketConnections map[*websocket.Conn]bool
	BadgerDB             *BadgerDB
	CounterBuffer        *PartitionedRingBuffer
	FlushInterval        time.Duration
	Wg                   sync.WaitGroup
	closing              int32
	BufferCriticalThreshold int
}

type CounterRequest struct {
	key   string
	count int64
}

func (req CounterRequest) hashCode() int {
	hash := 0
	for _, c := range req.key {
		hash = int(c) + ((hash << 5) - hash)
	}
	return hash
}

func IncrementCounterHandler(sharedData *SharedData) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&sharedData.closing) != 0 {
			http.Error(w, "Application is shutting down", http.StatusServiceUnavailable)
			return
		}

		key := r.FormValue("key")
		if key == "" {
			key = "default"
		}
		countStr := r.FormValue("count")
		count, err := strconv.ParseInt(countStr, 10, 64)
		if err != nil || count == 0 {
			http.Error(w, "Invalid or missing count value", http.StatusBadRequest)
			return
		}

		req := CounterRequest{key: key, count: count}
		sharedData.CounterBuffer.Add(req)

		if len(sharedData.CounterBuffer.RetrieveAll()) >= sharedData.BufferCriticalThreshold {
			go sharedData.FlushCounterBuffer()
		}

		response := map[string]string{"message": "Counter increment queued successfully"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func (sharedData *SharedData) RunCounterFlusher() {
	defer sharedData.Wg.Done()
	ticker := time.NewTicker(sharedData.FlushInterval)
	for {
		select {
		case <-ticker.C:
			err := sharedData.FlushCounterBuffer()
			if err != nil {
				fmt.Printf("Error flushing counter buffer: %v\n", err)
			}
		}
	}
}

func (sharedData *SharedData) FlushCounterBuffer() error {
	buffer := sharedData.CounterBuffer.RetrieveAll()
	if len(buffer) == 0 {
		return nil
	}

	for _, request := range buffer {
		// fmt.Printf("counter incremented by %d for key %s", request.count, request.key)
		err := sharedData.BadgerDB.IncrementCounter(request.key, request.count)
		if err != nil {
			return fmt.Errorf("failed to increment counter for key %s: %w", request.key, err)
		}
	}
	return nil
}

func (sharedData *SharedData) Start() error {
	sharedData.CounterBuffer = NewPartitionedRingBuffer(4, 100) // Partition into 4 segments
	sharedData.FlushInterval = time.Second * 5
	sharedData.BufferCriticalThreshold = 80

	sharedData.Wg.Add(1)
	go sharedData.RunCounterFlusher()

	return nil
}

func (sharedData *SharedData) Stop() {
	atomic.StoreInt32(&sharedData.closing, 1)
	sharedData.Wg.Wait()
	sharedData.BadgerDB.Close()
}

// Test and Mock
type BadgerDB struct{}

func (db *BadgerDB) IncrementCounter(key string, count int64) error {
	fmt.Printf("Incrementing counter %s by %d\n", key, count)
	return nil
}

func (db *BadgerDB) Close() {
	fmt.Println("Closing BadgerDB")
}

// func main() {
// 	sharedData := &SharedData{
// 		BadgerDB: &BadgerDB{},
// 	}

// 	err := sharedData.Start()
// 	if err != nil {
// 		fmt.Printf("Error starting shared data: %v\n", err)
// 		return
// 	}
// 	defer sharedData.Stop()

// 	http.HandleFunc("/increment", IncrementCounterHandler(sharedData))

// 	fmt.Println("Server starting on :8080")
// 	err = http.ListenAndServe(":8080", nil)
// 	if err != nil {
// 		fmt.Printf("Server error: %v\n", err)
// 	}
// }
