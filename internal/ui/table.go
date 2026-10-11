// Package ui renders human-readable tables and colors.
package ui

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table renders aligned, tab-separated columns using text/tabwriter.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable builds a table with the given headers.
func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

// AddRow appends a row to the table. Missing cells are rendered empty.
func (t *Table) AddRow(cells ...string) {
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
}

// Len returns the number of data rows.
func (t *Table) Len() int { return len(t.rows) }

// Render writes the table (header + rows) to w.
func (t *Table) Render(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.headers, "\t"))
	for _, r := range t.rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// Bool renders a boolean as a kubectl-style marker.
func Bool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
