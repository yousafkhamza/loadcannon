package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"time"
)

// k6 --summary-export shape (trimmed to the fields we render).
type metric struct {
	Type   string             `json:"type"`
	Values map[string]float64 `json:"values"`
}

type summary struct {
	Metrics map[string]metric `json:"metrics"`
}

const tmplSrc = `<!doctype html>
<html><head><meta charset="utf-8">
<title>loadcannon report</title>
<style>
body{font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#0b0f14;color:#e6edf3;padding:2rem;max-width:900px;margin:auto}
h1{color:#58a6ff} table{width:100%;border-collapse:collapse;margin-top:1rem}
th,td{text-align:left;padding:.5rem .75rem;border-bottom:1px solid #21262d}
th{color:#8b949e;font-weight:600;text-transform:uppercase;font-size:.75rem}
.pass{color:#3fb950} .fail{color:#f85149}
</style></head><body>
<h1>loadcannon — run report</h1>
<p>Generated {{ .Generated }}</p>
<table>
<tr><th>Metric</th><th>Value</th></tr>
{{- range .Rows }}
<tr><td>{{ .Name }}</td><td>{{ .Value }}</td></tr>
{{- end }}
</table>
</body></html>
`

type row struct{ Name, Value string }

// Render reads a k6 --summary-export JSON file and writes an HTML report.
func Render(summaryPath, outPath string) error {
	b, err := os.ReadFile(summaryPath)
	if err != nil {
		return fmt.Errorf("reading k6 summary %s: %w", summaryPath, err)
	}
	var s summary
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("parsing k6 summary: %w", err)
	}

	var rows []row
	order := []string{"http_reqs", "http_req_duration", "http_req_failed", "http_req_waiting", "iterations", "vus_max"}
	for _, name := range order {
		m, ok := s.Metrics[name]
		if !ok {
			continue
		}
		for _, key := range []string{"avg", "p(95)", "rate", "count", "value"} {
			if v, ok := m.Values[key]; ok {
				rows = append(rows, row{Name: name + " (" + key + ")", Value: fmt.Sprintf("%.2f", v)})
			}
		}
	}

	t, err := template.New("report").Parse(tmplSrc)
	if err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, struct {
		Generated string
		Rows      []row
	}{Generated: time.Now().UTC().Format(time.RFC1123), Rows: rows})
}
