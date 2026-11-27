package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Configuration page (admin/configure.php) framework. Each section lives in
// its own configure_<id>.go and registers a builder (what to show) and an
// applier (what the posted form changes), following docs/configure-spec.

// cfgControl is one form element. Kind: text, password, select, radio,
// toggle (Pi-Star toggle-round-flat checkbox: posts "ON", or "OFF" when
// unchecked), hidden, static (plain text), button (Text label, Attrs["action"]).
type cfgControl struct {
	Kind    string            `json:"kind"`
	Name    string            `json:"name,omitempty"`
	Value   string            `json:"value,omitempty"`
	Text    string            `json:"text,omitempty"`
	Options []linkOption      `json:"options,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// cfgTooltip is the classic <a class="tooltip">Label<span><b>Title</b>Body</span></a>;
// line breaks in Body are rendered as <br />.
type cfgTooltip struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type cfgCell struct {
	Header   bool         `json:"header,omitempty"` // <th>
	Colspan  int          `json:"colspan,omitempty"`
	Align    string       `json:"align,omitempty"`
	Style    string       `json:"style,omitempty"`
	Label    string       `json:"label,omitempty"`
	Tooltip  *cfgTooltip  `json:"tooltip,omitempty"`
	Controls []cfgControl `json:"controls,omitempty"`
}

type cfgRow struct {
	ID    string    `json:"id,omitempty"`
	Cells []cfgCell `json:"cells"`
}

type cfgSection struct {
	ID    string   `json:"id"`
	Title string   `json:"title"` // <h2>
	Rows  []cfgRow `json:"rows"`
	// Apply shows the section's "Apply Changes" button after the table.
	Apply bool `json:"apply"`
	// Form "config" (default) or a separate form such as "adminPassForm".
	Form string `json:"form,omitempty"`
}

// cfgContext gives builders and appliers the current state.
type cfgContext struct {
	St  configState
	Now time.Time
}

func (c *cfgContext) get(file, section, key string) string {
	return unquote(c.St.get(file, section, key))
}
func (c *cfgContext) has(file string) bool { return c.St.has(file) }
func (c *cfgContext) marker(name string) (string, bool) {
	m := c.St.System.Markers[name]
	if m == nil {
		return "", false
	}
	return *m, true
}

// cfgForm holds the posted values by control name.
type cfgForm map[string]string

func (f cfgForm) has(name string) bool { _, ok := f[name]; return ok }

// cfgPlan accumulates what Apply changes.
type cfgPlan struct {
	ctx    *cfgContext
	change configChange
	units  map[string]bool
}

func (p *cfgPlan) set(file, section, key, value string) {
	if p.change.Changes == nil {
		p.change.Changes = map[string]map[string]map[string]*string{}
	}
	if p.change.Changes[file] == nil {
		p.change.Changes[file] = map[string]map[string]*string{}
	}
	if p.change.Changes[file][section] == nil {
		p.change.Changes[file][section] = map[string]*string{}
	}
	p.change.Changes[file][section][key] = &value
}

func (p *cfgPlan) del(file, section, key string) {
	p.set(file, section, key, "")
	p.change.Changes[file][section][key] = nil
}

// value returns the planned value if any, else the stored one.
func (p *cfgPlan) value(file, section, key string) string {
	if v, ok := p.change.Changes[file][section][key]; ok {
		if v == nil {
			return ""
		}
		return unquote(*v)
	}
	return p.ctx.get(file, section, key)
}

func (p *cfgPlan) system(op string, value any) {
	if p.change.System == nil {
		p.change.System = map[string]any{}
	}
	p.change.System[op] = value
}

func (p *cfgPlan) restart(units ...string) {
	if p.units == nil {
		p.units = map[string]bool{}
	}
	for _, u := range units {
		p.units[u] = true
	}
}

// cfgError is shown to the operator; the whole Apply is refused.
type cfgError string

func (e cfgError) Error() string { return string(e) }

type cfgSectionImpl struct {
	ID    string
	Order int
	// Build returns nil when the classic page hides the section.
	Build func(c *cfgContext) *cfgSection
	// Apply reads only the controls Build rendered (absent names are left alone).
	Apply func(c *cfgContext, form cfgForm, p *cfgPlan) error
}

var cfgSections []cfgSectionImpl

func registerCfgSection(s cfgSectionImpl) {
	cfgSections = append(cfgSections, s)
	sort.SliceStable(cfgSections, func(i, j int) bool { return cfgSections[i].Order < cfgSections[j].Order })
}

// The classic Apply stops and restarts every radio service; the helper skips
// units that are not installed. Gateways first, controllers last.
var cfgRadioUnits = []string{
	"dmrgateway.service", "ysfgateway.service", "ysf2dmr.service", "ysf2nxdn.service", "ysf2p25.service",
	"dgidgateway.service", "ysfparrot.service", "p25gateway.service", "p25parrot.service", "nxdngateway.service",
	"nxdnparrot.service", "nxdn2dmr.service", "m17gateway.service", "dmr2ysf.service", "dmr2nxdn.service",
	"dapnetgateway.service", "aprsgateway.service", "mobilegps.service", "ircddbgateway.service",
	"timeserver.service", "pistar-remote.service", "pistar-watchdog.service",
	"mmdvmhost.service", "dstarrepeater.service",
}

type cfgPage struct {
	CSRF     string       `json:"csrf"`
	Revision string       `json:"revision"`
	Sections []cfgSection `json:"sections"`
}

func buildCfgPage(c *cfgContext) []cfgSection {
	var out []cfgSection
	for _, s := range cfgSections {
		if sec := s.Build(c); sec != nil {
			out = append(out, *sec)
		}
	}
	return out
}

// planCfgApply runs every visible section's applier and returns the helper request.
func planCfgApply(c *cfgContext, revision string, form cfgForm) (configChange, error) {
	p := &cfgPlan{ctx: c}
	p.change.Revision = revision
	for _, s := range cfgSections {
		if s.Build(c) == nil || s.Apply == nil {
			continue
		}
		if err := s.Apply(c, form, p); err != nil {
			return configChange{}, err
		}
	}
	// Drop no-op changes so an untouched form writes nothing.
	for file, sections := range p.change.Changes {
		for section, keys := range sections {
			for key, v := range keys {
				stored, present := c.St.Files[file][section][key]
				if (v == nil && !present) || (v != nil && present && *v == stored) {
					delete(keys, key)
				}
			}
			if len(keys) == 0 {
				delete(sections, section)
			}
		}
		if len(sections) == 0 {
			delete(p.change.Changes, file)
		}
	}
	if len(p.change.Changes) > 0 || len(p.change.System) > 0 || len(p.units) > 0 {
		p.change.Restart = append([]string{}, cfgRadioUnits...)
	}
	return p.change, nil
}

func serveConfigure(w http.ResponseWriter, r *http.Request, run configHelperFunc) {
	state, err := readConfigState(r.Context(), run)
	if err != nil {
		http.Error(w, "Не удалось прочитать конфигурацию", 502)
		return
	}
	c := &cfgContext{St: state, Now: time.Now()}
	switch r.Method {
	case http.MethodGet:
		csrf := editorToken(w, r)
		if csrf == "" {
			return
		}
		writeJSON(w, 200, cfgPage{CSRF: csrf, Revision: state.Revision, Sections: buildCfgPage(c)})
	case http.MethodPost:
		var in struct {
			Revision string  `json:"revision"`
			Form     cfgForm `json:"form"`
		}
		if !maintenancePost(w, r, &in) {
			return
		}
		req, err := planCfgApply(c, in.Revision, in.Form)
		var ce cfgError
		if errors.As(err, &ce) {
			http.Error(w, ce.Error(), 422)
			return
		} else if err != nil {
			http.Error(w, "Apply failed", 500)
			return
		}
		if req.Changes == nil && req.System == nil {
			writeJSON(w, 200, map[string]any{"ok": true, "changed": []string{}})
			return
		}
		body, _ := json.Marshal(req)
		out, err := run(r.Context(), "apply", body)
		switch {
		case errors.Is(err, errConfigStale):
			http.Error(w, "The configuration changed since this page was loaded; reload it", 409)
		case err != nil:
			var e struct{ Error string }
			_ = json.Unmarshal(out, &e)
			if e.Error == "" {
				e.Error = "Apply failed"
			}
			http.Error(w, e.Error, 422)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(out)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Метод не поддерживается", 405)
	}
}

// Helpers shared by the section files.

func tip(title, body string) *cfgTooltip { return &cfgTooltip{Title: title, Body: body} }

func th(label string, t *cfgTooltip) cfgCell { return cfgCell{Header: true, Label: label, Tooltip: t} }

func td(controls ...cfgControl) cfgCell { return cfgCell{Align: "left", Controls: controls} }

func text(name, value string, attrs map[string]string) cfgControl {
	return cfgControl{Kind: "text", Name: name, Value: value, Attrs: attrs}
}

func toggle(name string, on bool) cfgControl {
	v := "OFF"
	if on {
		v = "ON"
	}
	return cfgControl{Kind: "toggle", Name: name, Value: v}
}

func selectBox(name, value string, options []linkOption) cfgControl {
	return cfgControl{Kind: "select", Name: name, Value: value, Options: options}
}

func static(s string) cfgControl { return cfgControl{Kind: "static", Text: s} }

// on reports whether a posted toggle is ON.
func (f cfgForm) on(name string) bool { return strings.EqualFold(f[name], "ON") }

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
