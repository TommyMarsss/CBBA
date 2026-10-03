package cbba

import (
	"encoding/json"
	"strings"
	"testing"
)

// The replay page must be fully self-contained: no external scripts,
// stylesheets or fonts, and the embedded run data must round-trip.
func TestRenderHTMLSelfContained(t *testing.T) {
	robots, tasks := randomScenario(3, 5, 10)
	res, sim := Run(tasks, robots, Params{CostPerDist: 0.05}, UniformLoss(0.2), 3, 300, 3)
	html := RenderHTML(sim, res)

	for _, bad := range []string{"<script src", "<link ", "http://", "https://", "src="} {
		if strings.Contains(html, bad) {
			t.Fatalf("report is not self-contained: found %q", bad)
		}
	}

	// Extract the embedded JSON and verify it parses back to the run data.
	start := strings.Index(html, "const DATA = ")
	if start < 0 {
		t.Fatal("no embedded DATA in report")
	}
	end := strings.Index(html[start:], ";\n")
	if end < 0 {
		t.Fatal("embedded DATA not terminated")
	}
	raw := strings.TrimSpace(html[start+len("const DATA = ") : start+end])
	raw = strings.ReplaceAll(raw, `<\/`, "/")
	var decoded ReportData
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("embedded DATA is not valid JSON: %v", err)
	}
	if len(decoded.Tasks) != len(tasks) || len(decoded.Robots) != len(robots) {
		t.Fatalf("embedded data mismatch: %d tasks %d robots, want %d/%d",
			len(decoded.Tasks), len(decoded.Robots), len(tasks), len(robots))
	}
	if len(decoded.Rounds) == 0 {
		t.Fatal("no rounds in embedded data")
	}
	if decoded.Result.Converged != res.Converged {
		t.Fatalf("converged flag mismatch: %v vs %v", decoded.Result.Converged, res.Converged)
	}
}
