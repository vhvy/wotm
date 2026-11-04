package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wotm/internal/app"
	"wotm/internal/config"
	"wotm/web"
)

var version = "dev"

func main() {
	path := flag.String("config", "data/config.yaml", "configuration file")
	listen := flag.String("listen", ":8080", "HTTP listen address")
	init := flag.Bool("init", false, "create a configuration with a random password and exit")
	flag.Parse()
	if *init {
		if err := config.Initialize(*path); err != nil {
			log.Fatal(err)
		}
		log.Printf("Configuration created at %s; read web.password in that file to sign in", *path)
		return
	}
	store, err := config.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go store.Watch(ctx)
	assets, _ := fs.Sub(web.Assets, "assets/dist")
	server := &http.Server{Addr: *listen, Handler: app.New(store, assets, version), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("WOTM %s listening on %s", version, *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
