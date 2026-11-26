package main

import (
	_ "embed"
	"html/template"
	"io"
	"time"
)

//go:embed status.html
var statusHTML string

var statusPage = template.Must(template.New("status").Funcs(template.FuncMap{
	"stamp": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.UTC().Format("02.01.2006 15:04:05")
	},
	"local": func(rows []HeardRow) []HeardRow {
		var rf []HeardRow
		for _, row := range rows {
			if row.Source == "RF" {
				rf = append(rf, row)
			}
		}
		return rf
	},
}).Parse(statusHTML))

func render(w io.Writer, s Status) error { return statusPage.Execute(w, s) }
