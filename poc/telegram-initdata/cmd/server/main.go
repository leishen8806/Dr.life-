package main

import (
	"log"
	"net/http"
	"os"
	"time"

	telegraminitdata "drlife.local/poc/telegram-initdata"
)

func main() {
	verifier := telegraminitdata.PoolVerifier{Validator: telegraminitdata.NewValidator(), Pools: telegraminitdata.LoadPoolsFromEnv()}
	mux := http.NewServeMux()
	mux.Handle("/poc/", telegraminitdata.NewHandler(verifier))
	mux.Handle("/", http.FileServer(http.Dir("web")))
	server := &http.Server{
		Addr:              envOr("POC_HTTP_ADDR", ":8081"),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Telegram initData PoC listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
