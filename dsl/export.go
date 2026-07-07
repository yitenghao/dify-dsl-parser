package dsl

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
)

// EncodeOptions tunes the YAML emitter used by DSL.Marshal / WriteTo / WriteFile.
type EncodeOptions struct {
	// Indent is the number of spaces per nesting level. Defaults to 2 to
	// match Dify's own export style.
	Indent int
	// FreshEncoding, when true, drops every cached raw yaml.Node so all
	// payloads are re-encoded from the typed structs. Use this when you have
	// mutated typed fields without calling SetData / MarkDataDirty on each
	// affected node.
	FreshEncoding bool
}

// DefaultEncodeOptions returns the options used by the convenience entry
// points (Marshal, WriteFile, ...).
func DefaultEncodeOptions() EncodeOptions {
	return EncodeOptions{Indent: 2}
}

// Marshal serializes the DSL to YAML using DefaultEncodeOptions.
func (d *DSL) Marshal() ([]byte, error) {
	return d.MarshalWithOptions(DefaultEncodeOptions())
}

// MarshalWithOptions serializes the DSL to YAML with explicit options.
func (d *DSL) MarshalWithOptions(opts EncodeOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := d.encode(&buf, opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encode serializes the DSL to YAML and writes it to w using
// DefaultEncodeOptions.
//
// Note: this is intentionally not named WriteTo because that is reserved
// for io.WriterTo, which returns (int64, error).
func (d *DSL) Encode(w io.Writer) error {
	return d.encode(w, DefaultEncodeOptions())
}

// EncodeWithOptions serializes the DSL to YAML and writes it to w with
// explicit options.
func (d *DSL) EncodeWithOptions(w io.Writer, opts EncodeOptions) error {
	return d.encode(w, opts)
}

// WriteFile serializes the DSL to YAML and writes it to the given file path.
// The parent directory is created if missing.
func (d *DSL) WriteFile(path string) error {
	return d.WriteFileWithOptions(path, DefaultEncodeOptions())
}

// WriteFileWithOptions serializes the DSL to YAML and writes it to the given
// file path with explicit options.
func (d *DSL) WriteFileWithOptions(path string, opts EncodeOptions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := d.encode(f, opts); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// encode is the shared implementation used by all the export entry points.
func (d *DSL) encode(w io.Writer, opts EncodeOptions) error {
	if opts.FreshEncoding && d.IsWorkflow() && d.Workflow != nil {
		for i := range d.Workflow.Graph.Nodes {
			d.Workflow.Graph.Nodes[i].MarkDataDirty()
		}
	}
	enc := yaml.NewEncoder(w)
	indent := opts.Indent
	if indent <= 0 {
		indent = 2
	}
	enc.SetIndent(indent)
	if err := enc.Encode(d); err != nil {
		_ = enc.Close()
		return fmt.Errorf("encode dsl: %w", err)
	}
	return enc.Close()
}
