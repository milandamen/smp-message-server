package main

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait = 10 * time.Second
)

var (
	server *http.Server

	wsClientMutex *sync.Mutex
	wsClient      *Client
)

type Client struct {
	conn *websocket.Conn
}

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan

		ctx, cancelFunc := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelFunc()

		err := server.Shutdown(ctx)
		if err != nil {
			log.Println(err)
		}
	}()

	wsClientMutex = &sync.Mutex{}
	mux := http.NewServeMux()
	registerWebsocketHandlers(mux)
	registerMessageHandlers(mux)

	server = &http.Server{Addr: "localhost:8088", Handler: mux}
	err := server.ListenAndServe()
	if err != nil {
		log.Println(err)
	}
}

func (c *Client) Close() error {
	_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
	return c.conn.Close()
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func registerWebsocketHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/ws", func(rw http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(rw, r, nil)
		if err != nil {
			log.Println(err)
			return
		}

		wsClientMutex.Lock()
		defer wsClientMutex.Unlock()
		if wsClient != nil {
			if err := wsClient.Close(); err != nil {
				log.Println(err)
			}
			wsClient = nil
		}

		wsClient = &Client{conn: conn}
	})
}

func registerMessageHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/message", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = rw.Write([]byte("Only POST method is allowed."))
			return
		}
		if r.ContentLength <= 0 {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte("Request body cannot be empty."))
			return
		}

		rw.WriteHeader(http.StatusAccepted)

		wsClientMutex.Lock()
		defer wsClientMutex.Unlock()

		if wsClient != nil {
			conn := wsClient.conn
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Println(err)
				return
			}

			w, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Println(err)
				return
			}

			msg, err := ioutil.ReadAll(r.Body)
			if err != nil {
				log.Println(err)
				return
			}

			_, err = w.Write(msg)
			if err != nil {
				log.Println(err)
				return
			}

			if err := w.Close(); err != nil {
				log.Println(err)
				return
			}
		}
	})
}
