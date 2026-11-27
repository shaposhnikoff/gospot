package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
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
	config := flag.String("config", settingsPath, "dashboard settings file (pistar.conf)")
	listen := flag.String("listen", "", "HTTP status listen address (default :PANEL_PORT)")
	admin := flag.String("admin-listen", "", "serve only the HTTPS settings wizard at this address")
	operator := flag.String("operator-listen", "", "serve the native operator UI at this address")
	cert := flag.String("tls-cert", "", "wizard TLS certificate")
	key := flag.String("tls-key", "", "wizard TLS private key")
	flag.Parse()
	if err := loadSettings(*config); err != nil {
		log.Fatalf("Settings %s: %v", *config, err)
	}
	if *listen == "" {
		*listen = fmt.Sprintf(":%d", cfgPanelPort)
	}
	if *operator != "" {
		upstream := fmt.Sprintf("http://127.0.0.1:%d", cfgPanelPort)
		server := &http.Server{Addr: *operator, Handler: operatorHandler(operatorAssets, upstream, cfgManagementPort),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
			WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		log.Fatal(server.ListenAndServe())
	}
	if *admin != "" {
		mux := http.NewServeMux()
		registerWizardRoutes(mux, runWizardHelper, adminAssets)
		mux.HandleFunc("GET /runtime.json", func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Cache-Control", "no-store")
			operatorReply(writer, request, 200, "application/json", []byte(fmt.Sprintf(`{"operatorPort":%d}`, cfgOperatorPort)))
		})
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
	// One journalctl follower feeds last-heard, modem firmware and DMR login
	// state; pages never fork a log scan.
	go followRadioLogs(context.Background())
	go func() {
		for {
			s := collect()
			s.Radio = readRadio()
			rows, _, ok := recentHeard.live()
			s.Heard, s.HeardAvailable, s.UpdatedAt = rows, ok, time.Now().UTC()
			snapshot.Store(&s)
			time.Sleep(5 * time.Second)
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/dashboard", dashboardHandler())
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
