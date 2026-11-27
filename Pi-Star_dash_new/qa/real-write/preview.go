package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const previewCookie = "pistar-preview-csrf"
const upstreamCookie = "__Host-pistar-csrf"

func loopbackHost(authority string) bool {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		host = authority
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func previewHandler(assets string, upstream *url.URL, password string, roots *x509.CertPool) http.Handler {
	proxy := &httputil.ReverseProxy{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: 120 * time.Second},
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstream)
			request.Out.Host = upstream.Host
			request.Out.SetBasicAuth("pi-star", password)
			if request.In.Header.Get("Origin") != "" {
				request.Out.Header.Set("Origin", upstream.String())
			}
			request.Out.Header.Del("Cookie")
			if cookie, err := request.In.Cookie(previewCookie); err == nil {
				request.Out.AddCookie(&http.Cookie{Name: upstreamCookie, Value: cookie.Value})
			}
			if request.Out.URL.Path == "/wizard/" {
				request.Out.URL.Path = "/"
			}
		},
		ModifyResponse: func(response *http.Response) error {
			cookies := response.Cookies()
			response.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				if cookie.Name == upstreamCookie {
					cookie.Name, cookie.Secure = previewCookie, false
					response.Header.Add("Set-Cookie", cookie.String())
				}
			}
			if location := response.Header.Get("Location"); strings.HasPrefix(location, "/?done=") {
				response.Header.Set("Location", "/wizard/"+strings.TrimPrefix(location, "/"))
			}
			return nil
		},
	}
	static := http.FileServer(http.Dir(assets))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		if !loopbackHost(request.Host) || (request.Header.Get("Origin") != "" && request.Header.Get("Origin") != "http://"+request.Host) {
			http.Error(writer, "Local preview only", 403)
			return
		}
		if request.Method != "GET" && request.Method != "HEAD" && request.Method != "POST" {
			http.Error(writer, "Method not allowed", 405)
			return
		}
		path := request.URL.Path
		if request.Method == "POST" {
			if request.Header.Get("Origin") != "http://"+request.Host || (path != "/" && path != "/wizard/" && !strings.HasPrefix(path, "/manage/api/")) {
				http.Error(writer, "Invalid preview write", 403)
				return
			}
			proxy.ServeHTTP(writer, request)
			return
		}
		switch path {
		case "/runtime.json", "/manage/runtime.json":
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(writer, `{"backend":"pistar-go","preview":true}`)
			return
		case "/health":
			fmt.Fprintln(writer, "isolated-preview")
			return
		case "/api/status", "/api/last_heard.php":
			http.Error(writer, "Preview has no radio telemetry", 503)
			return
		}
		aliases := map[string]string{
			"/admin/": "/manage/", "/admin/index.php": "/manage/",
			"/admin/configure.php": "/manage/#configuration", "/admin/expert/": "/manage/#expert",
			"/admin/sysinfo.php": "/manage/#system", "/admin/live_modem_log.php": "/manage/#logs",
			"/admin/config_backup.php": "/manage/#backup", "/mmdvmhost/lh.php": "/#heard-all",
		}
		if target := aliases[path]; target != "" {
			http.Redirect(writer, request, target, http.StatusFound)
			return
		}
		if path == "/wizard/" || path == "/manage" || strings.HasPrefix(path, "/manage/") {
			proxy.ServeHTTP(writer, request)
			return
		}
		if path == "/" || path == "/index.html" || path == "/hoseline.html" || strings.HasPrefix(path, "/assets/") {
			static.ServeHTTP(writer, request)
			return
		}
		http.NotFound(writer, request)
	})
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8790", "loopback preview listener")
	upstreamValue := flag.String("upstream", "", "isolated fixture HTTPS origin")
	assets := flag.String("assets", "", "built native assets")
	credentials := flag.String("credentials", "", "private fixture credentials directory")
	flag.Parse()
	upstream, err := url.Parse(*upstreamValue)
	if err != nil || upstream.Scheme != "https" || !loopbackHost(upstream.Host) || upstream.Path != "" || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || !strings.HasPrefix(*listen, "127.0.0.1:") {
		log.Fatal("Preview requires explicit loopback addresses")
	}
	certificate, err := os.ReadFile(filepath.Join(*credentials, "cert.pem"))
	if err != nil {
		log.Fatal("Missing fixture certificate")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		log.Fatal("Invalid fixture certificate")
	}
	password, err := os.ReadFile(filepath.Join(*credentials, "admin-password"))
	if err != nil || len(strings.TrimSpace(string(password))) == 0 {
		log.Fatal("Missing fixture credential")
	}
	server := &http.Server{Addr: *listen, Handler: previewHandler(*assets, upstream, strings.TrimSpace(string(password)), roots),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 30 * time.Second}
	log.Fatal(server.ListenAndServe())
}
