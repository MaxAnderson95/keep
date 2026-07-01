package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// table is a tiny tabwriter-backed helper for aligned columns.
type table struct {
	w   *tabwriter.Writer
	out io.Writer
}

func newTable(out io.Writer) *table {
	return &table{
		w:   tabwriter.NewWriter(out, 0, 2, 2, ' ', 0),
		out: out,
	}
}

func (t *table) row(cols ...string) {
	fmt.Fprintln(t.w, strings.Join(cols, "\t"))
}

func (t *table) flush() {
	_ = t.w.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
