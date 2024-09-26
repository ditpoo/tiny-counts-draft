package main

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"

	"net/url"
	"sync"
	"testing"
	"time"

	"tiny-counts/db"
	"tiny-counts/handlers"
	"tiny-counts/models"

	"github.com/gorilla/websocket"
)

// go test -v ./... -bench=. -run=xxx -benchmem
func BenchmarkCounterIncrement(b *testing.B) {
	numHits := 1000000
	hitRate := 1000

	hitsChan := make(chan string, numHits)

	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(hitRate))
		defer ticker.Stop()
		for i := 0; i < numHits; i++ {
			hitsChan <- "hit"
		}
		close(hitsChan)
	}()

	badgerDB, err := db.NewBadgerDB("./tmp/badger")
	if err != nil {
		log.Fatal(err)
	}
	defer badgerDB.Close()

	sharedData := &handlers.SharedData{
		WebsocketConnections: make(map[*websocket.Conn]bool),
		BadgerDB:             badgerDB,
		Mutex:                sync.Mutex{},
		CounterBuffer:        make([]handlers.CounterRequest, 0, 100),
		CounterMutex:         &sync.Mutex{},
		FlushInterval:        time.Second * 5,
		Wg:                   sync.WaitGroup{},
	}

	sharedData.Wg.Add(1)
	go sharedData.RunCounterFlusher()

	flushCompleted := make(chan bool)
	go func() {
		time.Sleep(time.Second * 10)
		flushCompleted <- true
	}()

	start := time.Now()
	for range hitsChan {
		key := "test_count"
		count := 1
		req, err := http.NewRequest("POST", "/increment", nil)
		if err != nil {
			b.Fatal(err)
		}
		req.Form = url.Values{}
		req.Form.Set("key", key)
		req.Form.Set("count", strconv.FormatInt(int64(count), 10))
		w := httptest.NewRecorder()
		go handlers.IncrementCounterHandler(sharedData)(w, req)
	}
	<-flushCompleted
	elapsed := time.Since(start)

	expectedCounts := make(map[string]int64)
	for i := 0; i < numHits; i++ {
		expectedCounts["test_count"]++
	}
	actualCounts := make(map[string]int64)
	var allRecords []models.Record
	if err := sharedData.BadgerDB.Store.Find(&allRecords, nil); err != nil {
		log.Fatal(err)
	}
	for _, record := range allRecords {
		actualCounts[record.Key] = record.Count
	}

	for key, expected := range expectedCounts {
		actual := actualCounts[key]
		if actual != expected {
			b.Fatalf("Expected count for key %s to be %d, but got %d", key, expected, actual)
		}
	}

	b.Logf("Processed %d hits in %v", numHits, elapsed)
}
