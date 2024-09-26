package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"tiny-counts/db"
	"github.com/gorilla/websocket"
)

type SharedData struct {
	WebsocketConnections map[*websocket.Conn]bool
	Mutex                sync.Mutex
	BadgerDB             *db.BadgerDB     
	CounterBuffer        []CounterRequest
	CounterMutex         *sync.Mutex
	FlushInterval        time.Duration
	Wg                   sync.WaitGroup
	closing              bool
}

type CounterRequest struct {
	key   string
	count int64
}

func IncrementCounterHandler(sharedData *SharedData) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sharedData.closing {
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

		sharedData.CounterMutex.Lock()
		defer sharedData.CounterMutex.Unlock()
		sharedData.CounterBuffer = append(sharedData.CounterBuffer, CounterRequest{key: key, count: count})

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
	sharedData.CounterMutex.Lock()
	defer sharedData.CounterMutex.Unlock()

	if len(sharedData.CounterBuffer) == 0 {
		return nil
	}

	buffer := sharedData.CounterBuffer
	sharedData.CounterBuffer = make([]CounterRequest, 0, cap(sharedData.CounterBuffer))

	for _, request := range buffer {
		err := sharedData.BadgerDB.IncrementCounter(request.key, request.count)
		if err != nil {
			return fmt.Errorf("failed to increment counter for key %s: %w", request.key, err)
		}
	}

	return nil
}

func (sharedData *SharedData) Start() error {
	sharedData.CounterBuffer = make([]CounterRequest, 0, 100)
	sharedData.FlushInterval = time.Second * 5

	sharedData.Wg.Add(1)
	go sharedData.RunCounterFlusher()

	return nil
}

func (sharedData *SharedData) Stop() {
	sharedData.closing = true

	sharedData.Wg.Wait()

	sharedData.BadgerDB.Close()
}
