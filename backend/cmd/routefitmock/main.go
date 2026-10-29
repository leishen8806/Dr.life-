// routefitmock starts the test-only RouteFit substitute for local integration runs.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/testutil/routefitmock"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "HTTP listen address")
	secret := flag.String("callback-secret", "test-only-secret", "HMAC secret used when scripts deliver callbacks")
	flag.Parse()

	mock := routefitmock.New(routefitmock.Config{CallbackSecret: *secret})
	log.Printf("RouteFit test mock listening on %s", *listen)
	if err := http.ListenAndServe(*listen, mock.Handler()); err != nil {
		log.Fatal(err)
	}
}
