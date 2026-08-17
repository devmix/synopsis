package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"
)

// FillSummary is the database-fill part of the report.
type FillSummary struct {
	DurationMs float64          `json:"duration_ms"`
	Vectors    int              `json:"vectors"`
	Tables     map[string]int64 `json:"tables"` // rows per table after fill
}

// GraphSummary holds knowledge graph load metrics, reported separately from the tool benchmark.
type GraphSummary struct {
	LoadMs float64 `json:"load_ms"`
	Nodes  int     `json:"nodes"`
	Edges  int     `json:"edges"`
}

// Report is the complete result of one load-test run.
type Report struct {
	Scale        Scale         `json:"scale"`
	Seed         int64         `json:"seed,omitempty"`
	Filled       bool          `json:"filled"` // true when the database was populated by this run
	Iterations   int           `json:"iterations"`
	PagesPerCall int           `json:"pages_per_call"`
	Fill         *FillSummary  `json:"fill,omitempty"`
	Graph        *GraphSummary `json:"graph,omitempty"`
	Tools        []ToolStats   `json:"tools"`
}

// reportTableOrder fixes the display order of per-table row counts.
var reportTableOrder = []string{
	"documents", "chunks", "entities", "facts",
	"fact_sources", "entity_sources", "chunk_entities", "entity_links", "chunks_vec",
}

// Print writes a human-readable report to w and returns the first write error.
func (r *Report) Print(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Synopsis load test — scale=%s seed=%d filled=%v\n", r.Scale.Name, r.Seed, r.Filled); err != nil {
		return err
	}

	if r.Fill != nil {
		total := int64(0)
		for _, n := range r.Fill.Tables {
			total += n
		}
		if _, err := fmt.Fprintf(w, "\nFill: %.0f ms total, %d rows across tables, %d vectors embedded\n", r.Fill.DurationMs, total, r.Fill.Vectors); err != nil {
			return err
		}
		for _, table := range reportTableOrder {
			if n, ok := r.Fill.Tables[table]; ok {
				if _, err := fmt.Fprintf(w, "  %-15s %d\n", table+":", n); err != nil {
					return err
				}
			}
		}
	}

	if r.Graph != nil {
		if _, err := fmt.Fprintf(w, "\nKnowledge graph: loaded in %.0f ms — %d nodes, %d edges\n", r.Graph.LoadMs, r.Graph.Nodes, r.Graph.Edges); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "\nTool benchmark (iterations=%d, pages per call=%d):\n", r.Iterations, r.PagesPerCall); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 2, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TOOL\tCALLS\tPAGES\tERR\tAVG ms\tP50 ms\tP95 ms\tP99 ms\tMAX ms\tQPS"); err != nil {
		return err
	}
	for _, t := range r.Tools {
		pages := "-"
		if t.Pages > 0 {
			pages = strconv.Itoa(t.Pages)
		}
		name := t.Name
		if t.Errors > 0 { // flag rows whose latency stats exclude failed iterations
			name += " !"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.2f\n",
			name, t.Calls, pages, t.Errors, t.AvgMs, t.P50Ms, t.P95Ms, t.P99Ms, t.MaxMs, t.ThroughputQPS); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(w, "warning: flush report table: %v\n", err) //nolint:errcheck
	}

	for _, t := range r.Tools {
		if t.FirstError != "" {
			if _, err := fmt.Fprintf(w, "\nfirst error [%s]: %s\n", t.Name, t.FirstError); err != nil {
				return err
			}
		}
	}

	return nil
}

// WriteJSON writes the report as indented JSON to path.
func (r *Report) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report to %s: %w", path, err)
	}
	return nil
}
