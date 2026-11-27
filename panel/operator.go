package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// Set from DASHBOARD_UI_DIR by applySettings.
var operatorAssets string

var operatorPath = regexp.MustCompile(`^/[A-Za-z0-9_-][A-Za-z0-9_.-]*(/[A-Za-z0-9_-][A-Za-z0-9_.-]*)*$`)
var operatorHeard = regexp.MustCompile(`^/api/last_heard\.php(?:\?num_transmissions=[0-9]{1,3})?$`)

func operatorHandler(assetDir, upstream string, managementPort int) http.Handler {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var requests atomic.Uint64
	hose := newHoseline()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if path := request.URL.EscapedPath(); path == "/api/hoseline" || path == "/api/hoseline/groups" {
			hose.serve(writer, request)
			return
		}
		target := request.URL.RequestURI()
		switch target {
		case "/health":
			if _, err := os.Stat(filepath.Join(assetDir, "index.html")); err != nil {
				http.Error(writer, "Assets unavailable", http.StatusServiceUnavailable)
				return
			}
			operatorReply(writer, request, 200, "text/plain", []byte("ok\n"))
			return
		case "/metrics":
			operatorReply(writer, request, 200, "text/plain; version=0.0.4", []byte(fmt.Sprintf("# TYPE pistar_operator_requests_total counter\npistar_operator_requests_total %d\n", requests.Load())))
			return
		case "/runtime.json":
			operatorReply(writer, request, 200, "application/json", []byte(fmt.Sprintf(`{"backend":"pistar-go","wizardPort":%d}`, managementPort)))
			return
		case "/api/status", "/api/dashboard":
			operatorProxy(writer, request, client, upstream, target, true)
			return
		}
		if operatorHeard.MatchString(target) {
			operatorProxy(writer, request, client, upstream, target, false)
			return
		}
		if location := operatorRedirect(request, managementPort); location != "" {
			http.Redirect(writer, request, location, http.StatusFound)
			return
		}
		operatorStatic(writer, request, assetDir)
	})
}

func operatorReply(writer http.ResponseWriter, request *http.Request, status int, contentType string, body []byte) {
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
	writer.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = writer.Write(body)
	}
}

func operatorProxy(writer http.ResponseWriter, request *http.Request, client *http.Client, upstream, target string, status bool) {
	query, err := http.NewRequestWithContext(request.Context(), http.MethodGet, upstream+target, nil)
	if err != nil {
		http.Error(writer, "Status unavailable", 502)
		return
	}
	query.Header.Set("Accept", "application/json")
	response, err := client.Do(query)
	if err != nil {
		http.Error(writer, "Status unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == 503 && !status {
		operatorReply(writer, request, 503, "application/json", []byte("[]"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	var value any
	if response.StatusCode != 200 || err != nil || len(body) > 1<<20 || json.Unmarshal(body, &value) != nil {
		http.Error(writer, "Status unavailable", 502)
		return
	}
	_, isObject := value.(map[string]any)
	_, isArray := value.([]any)
	if (status && !isObject) || (!status && !isArray) {
		http.Error(writer, "Invalid status", 502)
		return
	}
	operatorReply(writer, request, 200, "application/json", body)
}

func operatorRedirect(request *http.Request, managementPort int) string {
	// The app's assets are relative to the page: /kiosk/ would resolve them
	// under /kiosk/assets/.
	public := map[string]string{"/index.php": "/", "/mmdvmhost/lh.php": "/", "/mmdvmhost/localtx.php": "/", "/mmdvmhost/repeaterinfo.php": "/", "/kiosk/": "/kiosk"}
	if location := public[request.URL.RequestURI()]; location != "" {
		return location
	}
	admin := map[string]string{"/admin/": "", "/admin/index.php": "", "/admin/configure.php": "#configuration", "/admin/expert/": "#expert", "/admin/live_modem_log.php": "#logs", "/admin/config_backup.php": "#backup", "/admin/sysinfo.php": "#system", "/admin/download_modem_log.php": "api/log/download"}
	fragment, found := admin[request.URL.RequestURI()]
	if !found {
		return ""
	}
	host := request.Host
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	if strings.ContainsAny(host, "/\\@?#\r\n\t ") || host == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(host, fmt.Sprint(managementPort)) + "/manage/" + fragment
}

func operatorStatic(writer http.ResponseWriter, request *http.Request, assetDir string) {
	path := request.URL.EscapedPath()
	// /kiosk is the wall-screen view of the same single-page app.
	if path == "/" || path == "/kiosk" {
		path = "/index.html"
	}
	types := map[string]string{".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2", ".webp": "image/webp"}
	contentType := types[filepath.Ext(path)]
	if !operatorPath.MatchString(path) || contentType == "" {
		http.NotFound(writer, request)
		return
	}
	root, err := filepath.EvalSymlinks(assetDir)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, path[1:]))
	if err != nil || !strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
		http.NotFound(writer, request)
		return
	}
	file, err := os.Open(candidate)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	http.ServeContent(writer, request, filepath.Base(path), info.ModTime(), file)
}
