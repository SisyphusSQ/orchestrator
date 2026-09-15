package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

type stats struct {
	Batches uint64 `json:"batches"`
	Bytes   uint64 `json:"bytes"`
}

func main() {
	listenAddress := flag.String("listen", "127.0.0.1:4318", "HTTP listen address")
	readyFile := flag.String("ready-file", "", "private file created after listen succeeds")
	flag.Parse()
	if *readyFile == "" {
		log.Fatal("--ready-file is required")
	}

	var batches atomic.Uint64
	var bytesReceived atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
		if err != nil {
			http.Error(w, "invalid trace payload", http.StatusBadRequest)
			return
		}
		digest := sha256.Sum256(body)
		batches.Add(1)
		bytesReceived.Add(uint64(len(body)))
		log.Printf("trace batch bytes=%d sha256=%s", len(body), hex.EncodeToString(digest[:]))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats{Batches: batches.Load(), Bytes: bytesReceived.Load()})
	})

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*readyFile, []byte(fmt.Sprintf("%s\n", listener.Addr())), 0600); err != nil {
		_ = listener.Close()
		log.Fatal(err)
	}

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-signals:
		log.Printf("received %s", sig)
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
}
