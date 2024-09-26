package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"tiny-counts/db"
	"tiny-counts/handlers"
	"tiny-counts/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/timshannon/badgerhold"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./tmp/badger"
	}
	bufferSize := os.Getenv("BUFFER_SIZE")
	if bufferSize == "" {
		bufferSize = "100"
	}
	flushInterval := os.Getenv("FLUSH_INTERVAL")
	if flushInterval == "" {
		flushInterval = "5s"
	}

	badgerDB, err := db.NewBadgerDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer badgerDB.Close()

	bufferSizeInt, err := strconv.Atoi(bufferSize)
	if err != nil {
		log.Fatalf("Invalid buffer size: %v", err)
	}
	flushIntervalDuration, err := time.ParseDuration(flushInterval)
	if err != nil {
		log.Fatalf("Invalid flush interval: %v", err)
	}

	sharedData := &handlers.SharedData{
		WebsocketConnections: make(map[*websocket.Conn]bool),
		BadgerDB:             badgerDB,
		Mutex:                sync.Mutex{},
		CounterBuffer:        make([]handlers.CounterRequest, 0, bufferSizeInt),
		CounterMutex:         &sync.Mutex{},
		FlushInterval:        flushIntervalDuration,
		Wg:                   sync.WaitGroup{},
	}

	// Start the worker for flushing the buffer
	sharedData.Wg.Add(1)
	go sharedData.RunCounterFlusher()

	// Router
	router := mux.NewRouter()
	router.HandleFunc("/increment", handlers.IncrementCounterHandler(sharedData)).Methods("POST")
	router.HandleFunc("/aggregate", handlers.AggregateCountsHandler(sharedData)).Methods("GET")
	router.HandleFunc("/", handlers.HomeHandler(sharedData)).Methods("GET")
	router.HandleFunc("/ws", wsEndpoint(sharedData))

	// Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

func wsEndpoint(sharedData *handlers.SharedData) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Error upgrading to WebSocket:", err)
			return
		}
		defer conn.Close()

		sharedData.Mutex.Lock()
		sharedData.WebsocketConnections[conn] = true
		sharedData.Mutex.Unlock()

		go func() {
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					sharedData.Mutex.Lock()
					delete(sharedData.WebsocketConnections, conn)
					sharedData.Mutex.Unlock()
					break
				}
			}
		}()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			var allRecords []models.Record
			if err := sharedData.BadgerDB.Store.Find(&allRecords, nil); err != nil && err != badgerhold.ErrNotFound {
				log.Println("Error getting total counts:", err)
				continue
			}

			allTotals := make(map[string]int64)
			for _, record := range allRecords {
				allTotals[record.Key] += record.Count
			}

			sharedData.Mutex.Lock()
			for client := range sharedData.WebsocketConnections {
				err := client.WriteJSON(allTotals)
				if err != nil {
					log.Println("Error writing to WebSocket:", err)
					client.Close()
					delete(sharedData.WebsocketConnections, client)
				}
			}
			sharedData.Mutex.Unlock()
		}
	}
}
