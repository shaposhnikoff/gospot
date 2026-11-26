package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

type Signal struct {
	State, Detail, Source string
	At                    time.Time
}

type Status struct {
	Node           string
	Modem, Master  Signal
	Radio          RadioInfo
	Heard          []HeardRow
	HeardAvailable bool
	UpdatedAt      time.Time
}

func main() {
	listen := flag.String("listen", ":8787", "HTTP listen address")
	admin := flag.String("admin-listen", "", "serve only the HTTPS settings wizard at this address")
	cert := flag.String("tls-cert", "", "wizard TLS certificate")
	key := flag.String("tls-key", "", "wizard TLS private key")
	flag.Parse()
	if *admin != "" {
		mux := http.NewServeMux()
		registerWizard(mux)
		srv := &http.Server{
			Addr: *admin, Handler: mux,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
			WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second,
			MaxHeaderBytes: 16 << 10,
			TLSConfig:      &tls.Config{MinVersion: tls.VersionTLS12},
		}
		log.Fatal(srv.ListenAndServeTLS(*cert, *key))
	}
	var snapshot atomic.Pointer[Status]
	go func() {
		var rows []HeardRow
		var ok bool
		var next time.Time
		for {
			s := collect()
			s.Radio = readRadio()
			if !time.Now().Before(next) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				rows, ok = collectHeard(ctx)
				cancel()
				next = time.Now().Add(15 * time.Second)
			}
			s.Heard, s.HeardAvailable, s.UpdatedAt = rows, ok, time.Now().UTC()
			snapshot.Store(&s)
			time.Sleep(5 * time.Second)
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/last_heard.php", func(w http.ResponseWriter, r *http.Request) {
		serveLegacyHeard(w, r, snapshot.Load())
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		s := snapshot.Load()
		if s == nil {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "Сбор первого снимка", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s)
			return
		}
		var b bytes.Buffer
		if err := render(&b, *s); err != nil {
			http.Error(w, "Ошибка отображения", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = b.WriteTo(w)
	})
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
