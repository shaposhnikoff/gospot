package main

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"reflect"
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

// modemSet stages a change of the active controller marker's INI (the
// [BrandMeister]/[TGIF] passwords) as system op "modem_ini"; nil removes the
// key. Sections merge their changes into one operation.
func (p *cfgPlan) modemSet(section, key string, value *string) {
	ops, _ := p.change.System["modem_ini"].(map[string]map[string]*string)
	if ops == nil {
		ops = map[string]map[string]*string{}
	}
	if ops[section] == nil {
		ops[section] = map[string]*string{}
	}
	ops[section][key] = value
	p.system("modem_ini", ops)
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
	"timeserver.service", "gospot-remote.service", "gospot-watchdog.service",
	"mmdvmhost.service", "dstarrepeater.service",
}

// System operations that touch no radio configuration: the classic password
// and Auto AP PSK forms restart nothing.
var cfgQuietOps = map[string]bool{"password": true, "autoap_psk": true}

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

// runCfgAppliers runs every visible section's applier on form.
func runCfgAppliers(c *cfgContext, revision string, form cfgForm) (*cfgPlan, error) {
	p := &cfgPlan{ctx: c}
	p.change.Revision = revision
	for _, s := range cfgSections {
		if s.Build(c) == nil || s.Apply == nil {
			continue
		}
		if err := s.Apply(c, form, p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// cfgDisplayed is the value every named control shows on the page (the first
// control wins, as in Configure.svelte).
func cfgDisplayed(c *cfgContext) map[string]string {
	out := map[string]string{}
	for _, s := range buildCfgPage(c) {
		for _, row := range s.Rows {
			for _, cell := range row.Cells {
				for _, ctl := range cell.Controls {
					switch ctl.Kind {
					case "static", "button", "link":
						continue
					}
					if _, seen := out[ctl.Name]; ctl.Name != "" && !seen {
						out[ctl.Name] = ctl.Value
					}
				}
			}
		}
	}
	return out
}

// planCfgRaw is what the appliers stage for form, minus anything already
// stored, with the restart decision; section tests exercise it directly.
func planCfgRaw(c *cfgContext, revision string, form cfgForm) (configChange, error) {
	p, err := runCfgAppliers(c, revision, form)
	if err != nil {
		return configChange{}, err
	}
	dropStored(c, &p.change)
	finishCfgPlan(&p.change)
	return p.change, nil
}

// planCfgApply returns the helper request for a posted form. Only what the
// operator's edits cause is kept: a change the same form would also plan with
// every field left as displayed (classic defaults and normalisation the
// appliers stage on any save) is dropped, as is anything already stored, so
// an untouched form writes and restarts nothing.
func planCfgApply(c *cfgContext, revision string, form cfgForm) (configChange, error) {
	p, err := runCfgAppliers(c, revision, form)
	if err != nil {
		return configChange{}, err
	}
	shown := cfgDisplayed(c)
	baseForm := cfgForm{}
	for k, v := range form {
		if d, ok := shown[k]; ok {
			v = d
		}
		baseForm[k] = v
	}
	base, err := runCfgAppliers(c, revision, baseForm)
	if err != nil {
		base = &cfgPlan{ctx: c}
	}
	dropStored(c, &p.change)
	staged := map[string]map[string]map[string]*string{}
	for file, sections := range p.change.Changes {
		staged[file] = map[string]map[string]*string{}
		for section, keys := range sections {
			staged[file][section] = maps.Clone(keys)
		}
	}
	same := func(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	for file, sections := range p.change.Changes {
		for section, keys := range sections {
			for key, v := range keys {
				if bv, staged := base.change.Changes[file][section][key]; staged && same(v, bv) {
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
	// A file the edit changes also gets the classic changes to keys it
	// already has (migrations such as dropping YSFGateway's old [aprs.fi]),
	// so it is never left half-migrated; defaults for missing keys are still
	// not added.
	for file := range p.change.Changes {
		for section, keys := range staged[file] {
			for key, v := range keys {
				if _, present := c.St.Files[file][section][key]; !present {
					continue
				}
				if p.change.Changes[file][section] == nil {
					p.change.Changes[file][section] = map[string]*string{}
				}
				p.change.Changes[file][section][key] = v
			}
		}
	}
	for op, v := range p.change.System {
		if bv, staged := base.change.System[op]; staged && reflect.DeepEqual(v, bv) {
			delete(p.change.System, op)
		}
	}
	finishCfgPlan(&p.change)
	return p.change, nil
}

// dropStored removes changes that match the files already.
func dropStored(c *cfgContext, ch *configChange) {
	for file, sections := range ch.Changes {
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
			delete(ch.Changes, file)
		}
	}
}

// finishCfgPlan normalises empty maps and decides the restart: any file
// change or a system operation other than the quiet ones restarts the radio.
func finishCfgPlan(ch *configChange) {
	if len(ch.Changes) == 0 {
		ch.Changes = nil
	}
	if len(ch.System) == 0 {
		ch.System = nil
	}
	restart := len(ch.Changes) > 0
	for op := range ch.System {
		restart = restart || !cfgQuietOps[op]
	}
	ch.Restart = nil
	if restart {
		ch.Restart = append([]string{}, cfgRadioUnits...)
	}
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
