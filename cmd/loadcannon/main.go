// loadcannon is a small, dependency-free CLI that wraps k6 to load-test
// internal and public HTTP APIs from one consistent config format, with
// secrets resolved securely at run time (env, file, AWS SSM, or a masked
// prompt) instead of being hardcoded in the scenario file.
package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/yousafkhamza/loadcannon/internal/auth"
	"github.com/yousafkhamza/loadcannon/internal/config"
	"github.com/yousafkhamza/loadcannon/internal/examples"
	"github.com/yousafkhamza/loadcannon/internal/k6gen"
	"github.com/yousafkhamza/loadcannon/internal/report"
	"github.com/yousafkhamza/loadcannon/internal/runner"
)

// Version is set at build time via -ldflags "-X main.Version=..." (see .goreleaser.yml)
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "examples":
		cmdExamples(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "validate":
		cmdValidate(os.Args[2:])
	case "gen-k6":
		cmdGenK6(os.Args[2:])
	case "report":
		cmdReport(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("loadcannon " + Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`loadcannon — load test internal and public APIs behind one config

New here? Run these in order:
  1. loadcannon examples --write scenarios                    copies example scenarios into ./scenarios
  2. cp scenarios/example-public-https-domain.json my-api.json   pick the closest match, then edit my-api.json
  3. loadcannon validate --scenario my-api.json               sanity check: resolves auth, fires ONE request
  4. loadcannon run --scenario my-api.json                    runs the real load test via k6
  5. open loadcannon-out/report.html                          view the results

Commands:
  loadcannon examples [--write <dir>]             list bundled examples, or copy them to <dir> (default: current dir)
  loadcannon validate --scenario <file>           resolve auth + fire one baseline request, no load
  loadcannon run      --scenario <file> [flags]   generate + execute a k6 run
  loadcannon gen-k6    --scenario <file> -o <file>  write the k6 script only, don't run it
  loadcannon report    --summary <file> -o <file>   render an HTML report from a k6 summary
  loadcannon version

Run flags:
  --scenario <file>   scenario JSON file (required)
  --vus <n>            override load.vus for a flat (non-staged) run
  --duration <dur>     override load.duration, e.g. 2m
  --out <dir>          output directory for script/summary/report (default ./loadcannon-out)
  --k6-arg <arg>        pass an extra raw argument through to k6 (repeatable)

Full docs, scenario file schema, and secret-handling model:
  https://github.com/yousafkhamza/loadcannon`)
}

func mustParseScenario(path string) *config.Config {
	if path == "" {
		fmt.Fprintln(os.Stderr, "error: --scenario is required")
		os.Exit(1)
	}
	c, err := config.Load2(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return c
}

func flagVal(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func flagAll(args []string, name string) []string {
	var out []string
	for i, a := range args {
		if a == name && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func cmdExamples(args []string) {
	writeDir := flagVal(args, "--write")
	if writeDir == "" {
		names, err := examples.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Bundled example scenarios (run `loadcannon examples --write scenarios` to copy them to disk):")
		for _, n := range names {
			fmt.Println("  " + n)
		}
		return
	}
	written, err := examples.Write(writeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for _, w := range written {
		fmt.Println("wrote " + w)
	}
	if len(written) == 0 {
		fmt.Println("nothing to write — all examples already exist in " + writeDir)
	} else {
		fmt.Println("\nnext: pick the closest match, edit it for your API, then `loadcannon validate --scenario <file>`")
	}
}

func cmdValidate(args []string) {
	c := mustParseScenario(flagVal(args, "--scenario"))

	if c.Target.Type == config.TargetInternal {
		fmt.Println("[info] target.type=internal — make sure this host can reach the endpoint")
		fmt.Println("       (VPN connected, running inside the VPC, or via scripts/tunnel-ssm.sh)")
	}

	env, err := auth.BuildAuthEnv(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth resolution failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ok] auth resolved (" + string(c.Auth.AuthMode) + ")")

	url := c.Target.URL + c.Scenarios[0].Path
	req, err := http.NewRequest(c.Scenarios[0].Method, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "building baseline request: %v\n", err)
		os.Exit(1)
	}
	if c.Target.HostOverride != "" {
		req.Host = c.Target.HostOverride
	}
	if v, ok := env["LOADCANNON_AUTH_HEADER"]; ok {
		req.Header.Set(v, env["LOADCANNON_AUTH_VALUE"])
	}
	if u, ok := env["LOADCANNON_AUTH_USER"]; ok {
		req.SetBasicAuth(u, env["LOADCANNON_AUTH_PASS"])
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: c.Target.InsecureSkipVerify,
				ServerName:         serverNameFor(c),
			},
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fail] baseline request to %s failed: %v\n", url, err)
		if c.Target.Type == config.TargetInternal {
			fmt.Fprintln(os.Stderr, "        (this is expected if you're not on the internal network yet)")
		}
		os.Exit(1)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	fmt.Printf("[ok] baseline: %s %s -> %d in %s\n", c.Scenarios[0].Method, url, resp.StatusCode, elapsed)
	if resp.StatusCode != c.Scenarios[0].ExpectStatus {
		fmt.Printf("[warn] expected status %d, got %d — check auth/path before running full load\n", c.Scenarios[0].ExpectStatus, resp.StatusCode)
	}
}

func serverNameFor(c *config.Config) string {
	if c.Target.HostOverride != "" {
		return c.Target.HostOverride
	}
	return ""
}

func cmdGenK6(args []string) {
	c := mustParseScenario(flagVal(args, "--scenario"))
	script, err := k6gen.Generate(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	out := flagVal(args, "-o")
	if out == "" {
		fmt.Print(script)
		return
	}
	if err := os.WriteFile(out, []byte(script), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", out, err)
		os.Exit(1)
	}
	fmt.Println("wrote " + out)
}

func cmdRun(args []string) {
	c := mustParseScenario(flagVal(args, "--scenario"))

	if v := flagVal(args, "--vus"); v != "" {
		fmt.Sscanf(v, "%d", &c.Load.VUs)
		c.Load.Stages = nil
	}
	if v := flagVal(args, "--duration"); v != "" {
		c.Load.Duration = v
		c.Load.Stages = nil
	}

	outDir := flagVal(args, "--out")
	if outDir == "" {
		outDir = "./loadcannon-out"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	script, err := k6gen.Generate(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	scriptPath := outDir + "/script.js"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	env, err := auth.BuildAuthEnv(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth resolution failed: %v\n", err)
		os.Exit(1)
	}

	summaryPath := outDir + "/summary.json"
	if err := runner.Run(scriptPath, summaryPath, env, flagAll(args, "--k6-arg")); err != nil {
		fmt.Fprintf(os.Stderr, "k6 run failed: %v\n", err)
		os.Exit(1)
	}

	reportPath := outDir + "/report.html"
	if err := report.Render(summaryPath, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: report generation failed: %v\n", err)
		return
	}
	fmt.Println("report: " + reportPath)
}

func cmdReport(args []string) {
	summaryPath := flagVal(args, "--summary")
	out := flagVal(args, "-o")
	if summaryPath == "" || out == "" {
		fmt.Fprintln(os.Stderr, "usage: loadcannon report --summary <file> -o <file>")
		os.Exit(1)
	}
	if err := report.Render(summaryPath, out); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote " + out)
}
