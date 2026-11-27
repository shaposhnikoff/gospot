package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Expert > Quick Edit (admin/expert/edit_*.php): every section of one radio
// configuration file as key/value inputs; Apply writes the keys back and
// restarts the file's service. Dispatch only after wizardHandler's checks.

type quickFile struct {
	unit string
	// flat files have no sections; the classic editor shows them under one
	// heading named after the file and writes empty values as "key= ".
	flat bool
}

var quickEditFiles = map[string]quickFile{
	"dstarrepeater": {"dstarrepeater.service", true}, "ircddbgateway": {"ircddbgateway.service", true},
	"timeserver": {"timeserver.service", true}, "starnetserver": {"starnetserver.service", true},
	"mmdvmhost": {"mmdvmhost.service", false}, "dmrgateway": {"dmrgateway.service", false},
	"ysfgateway": {"ysfgateway.service", false}, "p25gateway": {"p25gateway.service", false},
	"nxdngateway": {"nxdngateway.service", false}, "dapnetgateway": {"dapnetgateway.service", false},
}

type quickSection struct {
	Name string      `json:"name"`
	Keys [][2]string `json:"keys"`
}

type quickReply struct {
	CSRF     string         `json:"csrf"`
	Revision string         `json:"revision"`
	File     string         `json:"file"`
	Sections []quickSection `json:"sections"`
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

// quickValue applies the classic edit_*.php write rules.
func quickValue(file, section, key, value string) string {
	quoted := func() string { return `"` + strings.ReplaceAll(value, `"`, "") + `"` }
	switch file {
	case "mmdvmhost":
		switch {
		case section == "DMR Network" && key == "Password" && value != "":
			return quoted()
		case section == "DMR Network" && key == "Options" && value != "":
			return quoted()
		case section == "DMR Network" && key == "Options":
			return " "
		case value == "":
			return "none"
		}
	case "dmrgateway":
		if (section == "DMR Network 1" || section == "DMR Network 2") && (key == "Password" || key == "Options") && value != "" {
			return quoted()
		}
	default:
		if quickEditFiles[file].flat && value == "" {
			return " "
		}
	}
	return value
}

var fullEditFiles = map[string]bool{"dmrgateway": true, "pistar-remote": true, "wpaconfig": true, "cron": true, "rssidat": true}

// serveFullEdit: admin/expert/fulledit_*.php, the whole file in a textarea.
func serveFullEdit(w http.ResponseWriter, r *http.Request, run configHelperFunc, name string) {
	if !fullEditFiles[name] {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		out, err := run(r.Context(), "full-read "+name, nil)
		var reply map[string]any
		if err != nil || json.Unmarshal(out, &reply) != nil {
			http.Error(w, "Could not read the file", 502)
			return
		}
		reply["csrf"] = csrf
		writeJSON(w, 200, reply)
	case http.MethodPost:
		var in struct {
			Revision string `json:"revision"`
			Text     string `json:"text"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		body, _ := json.Marshal(in)
		out, err := run(r.Context(), "full-write "+name, body)
		var reply map[string]any
		_ = json.Unmarshal(out, &reply)
		switch {
		case errors.Is(err, errConfigStale):
			http.Error(w, "The file changed since this page was loaded; reload it", 409)
		case err != nil:
			msg, _ := reply["error"].(string)
			if msg == "" {
				msg = "Save failed"
			}
			http.Error(w, msg, 422)
		default:
			writeJSON(w, 200, reply)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

func serveExpert(w http.ResponseWriter, r *http.Request, run configHelperFunc) {
	name := strings.TrimPrefix(r.URL.EscapedPath(), "/manage/api/expert/")
	if name == "css" {
		serveCSSTool(w, r, run)
		return
	}
	if name == "keys" || name == "fw" || name == "ssh" {
		serveExpertTools(w, r, run, runSudo, name)
		return
	}
	if full, ok := strings.CutPrefix(name, "full/"); ok {
		serveFullEdit(w, r, run, full)
		return
	}
	qf, ok := quickEditFiles[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := readConfigState(r.Context(), run)
	if err != nil {
		http.Error(w, "Не удалось прочитать конфигурацию", 502)
		return
	}
	layout, installed := state.Layout[name]
	if !installed {
		http.Error(w, name+" is not installed", 404)
		return
	}
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		reply := quickReply{CSRF: csrf, Revision: state.Revision, File: name}
		for _, s := range layout {
			sec := quickSection{Name: s.Name}
			if qf.flat {
				sec.Name = name
			}
			for _, kv := range s.Keys {
				sec.Keys = append(sec.Keys, [2]string{kv[0], unquote(kv[1])})
			}
			reply.Sections = append(reply.Sections, sec)
		}
		writeJSON(w, 200, reply)
	case http.MethodPost:
		var in struct {
			Revision string                       `json:"revision"`
			Values   map[string]map[string]string `json:"values"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		changes := map[string]map[string]*string{}
		for _, s := range layout {
			section := s.Name
			form := in.Values[section]
			if qf.flat {
				form = in.Values[name]
			}
			for _, kv := range s.Keys {
				value, sent := form[kv[0]]
				// Untouched fields never change the file: the classic write
				// rules apply only to values the operator edited.
				if !sent || value == unquote(kv[1]) {
					continue
				}
				v := quickValue(name, section, kv[0], value)
				if v == kv[1] {
					continue
				}
				if changes[section] == nil {
					changes[section] = map[string]*string{}
				}
				changes[section][kv[0]] = &v
			}
		}
		req := configChange{Revision: in.Revision}
		if len(changes) > 0 {
			req.Changes = map[string]map[string]map[string]*string{name: changes}
			req.Restart = []string{qf.unit}
		}
		body, _ := json.Marshal(req)
		out, err := run(r.Context(), "apply", body)
		switch {
		case errors.Is(err, errConfigStale):
			http.Error(w, "The file changed since this page was loaded; reload it", 409)
		case err != nil:
			var e struct{ Error string }
			_ = json.Unmarshal(out, &e)
			if e.Error == "" {
				e.Error = "Apply failed"
			}
			http.Error(w, e.Error, 422)
		default:
			writeJSON(w, 200, map[string]bool{"ok": true})
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}
