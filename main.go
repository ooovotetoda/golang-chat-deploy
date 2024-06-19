package main

import (
	"database/sql"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

type Message struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
	Room   string `json:"room"`
}

type Room struct {
	Clients map[*websocket.Conn]bool
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var rooms = make(map[string]*Room)
var broadcast = make(chan Message)
var mutex = &sync.Mutex{}

func initDB() *sql.DB {
	db, err := sql.Open("sqlite3", "./chat.db")
	if err != nil {
		log.Fatal(err)
	}

	createTableQuery := `CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		author TEXT,
		text TEXT,
		room TEXT
	);`

	_, err = db.Exec(createTableQuery)
	if err != nil {
		log.Fatal(err)
	}

	return db
}

func wsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		roomID := r.URL.Query().Get("room")
		if roomID == "" {
			http.Error(w, "Room ID is required", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}
		defer conn.Close()

		mutex.Lock()
		if _, ok := rooms[roomID]; !ok {
			rooms[roomID] = &Room{Clients: make(map[*websocket.Conn]bool)}
		}
		rooms[roomID].Clients[conn] = true
		mutex.Unlock()

		rows, err := db.Query("SELECT id, author, text, room FROM messages WHERE room = ?", roomID)
		if err != nil {
			log.Println("Database query error:", err)
		}
		defer rows.Close()

		var messages []Message
		for rows.Next() {
			var msg Message
			err := rows.Scan(&msg.ID, &msg.Author, &msg.Text, &msg.Room)
			if err != nil {
				log.Println("Database scan error:", err)
			}
			messages = append(messages, msg)
		}

		err = conn.WriteJSON(messages)
		if err != nil {
			log.Println("Write error:", err)
			conn.Close()
			mutex.Lock()
			delete(rooms[roomID].Clients, conn)
			if len(rooms[roomID].Clients) == 0 {
				delete(rooms, roomID)
			}
			mutex.Unlock()
			return
		}

		for {
			var msg Message
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Println("Read error:", err)
				mutex.Lock()
				delete(rooms[roomID].Clients, conn)
				if len(rooms[roomID].Clients) == 0 {
					delete(rooms, roomID)
				}
				mutex.Unlock()
				break
			}
			msg.ID = uuid.New().String()
			msg.Room = roomID
			broadcast <- msg
		}
	}
}

func handleMessages(db *sql.DB) {
	for {
		msg := <-broadcast
		mutex.Lock()
		if room, ok := rooms[msg.Room]; ok {
			for client := range room.Clients {
				err := client.WriteJSON(msg)
				if err != nil {
					log.Println("Write error:", err)
					client.Close()
					delete(room.Clients, client)
					if len(room.Clients) == 0 {
						delete(rooms, msg.Room)
					}
				}
			}
		}
		mutex.Unlock()

		_, err := db.Exec("INSERT INTO messages (id, author, text, room) VALUES (?, ?, ?, ?)", msg.ID, msg.Author, msg.Text, msg.Room)
		if err != nil {
			log.Println("Database insert error:", err)
		}
	}
}

func main() {
	db := initDB()
	defer db.Close()

	http.HandleFunc("/ws", wsHandler(db))
	go handleMessages(db)

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
