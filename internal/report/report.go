package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"time"
)

// k6's --summary-export JSON has varied by version: older/current builds may
// emit a flat shape per metric ({"avg":1,"p(95)":2,...}), while the shape
// documented for the handleSummary() `data` argument nests stats under
// "values" ({"type":"trend","values":{"avg":1,"p(95)":2,...}}). The
// --summary-export flag has been marked unsupported/deprecated since k6
// v0.30 with no stable schema guarantee, so rather than pin to one shape
// (which silently produced an empty report against a real k6 v2.0 run),
// this detects whichever shape is present per metric.
type summary struct {
	Metrics map[string]json.RawMessage `json:"metrics"`
}

type nestedMetric struct {
	Values map[string]float64 `json:"values"`
}

// metricValues returns the stat->number map for one metric's raw JSON,
// trying the nested {values:{...}} shape first and falling back to treating
// the object itself as the flat stat map.
func metricValues(raw json.RawMessage) map[string]float64 {
	var nested nestedMetric
	if err := json.Unmarshal(raw, &nested); err == nil && len(nested.Values) > 0 {
		return nested.Values
	}
	var flat map[string]float64
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat
	}
	return nil
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
		raw, ok := s.Metrics[name]
		if !ok {
			continue
		}
		values := metricValues(raw)
		for _, key := range []string{"avg", "p(95)", "rate", "count", "value"} {
			if v, ok := values[key]; ok {
				rows = append(rows, row{Name: name + " (" + key + ")", Value: fmt.Sprintf("%.2f", v)})
			}
		}
	}

	if len(rows) == 0 {
		return fmt.Errorf("no recognized metrics found in %s — the k6 summary format may have changed; run with --k6-arg --verbose and check %s directly", summaryPath, summaryPath)
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
