// Package promparse reads the subset of the Prometheus text exposition format
// that llama.cpp's /metrics endpoint emits.
package promparse

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// Sample is a single metric series: a name, its label set, and a value.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Family groups every series sharing a metric name.
type Family struct {
	Name    string
	Help    string
	Type    string
	Samples []Sample
}

// Set is a parsed exposition document keyed by metric name.
type Set map[string]*Family

// Value returns the value of an unlabeled series, and whether it was present.
// Series carrying labels are skipped so a labeled histogram cannot be mistaken
// for a scalar.
func (s Set) Value(name string) (float64, bool) {
	f, ok := s[name]
	if !ok {
		return 0, false
	}
	for _, sample := range f.Samples {
		if len(sample.Labels) == 0 {
			return sample.Value, true
		}
	}
	return 0, false
}

// ValueOr returns the value of an unlabeled series, or def if absent.
func (s Set) ValueOr(name string, def float64) float64 {
	if v, ok := s.Value(name); ok {
		return v
	}
	return def
}

// Samples returns every series recorded under name.
func (s Set) Samples(name string) []Sample {
	if f, ok := s[name]; ok {
		return f.Samples
	}
	return nil
}

// Parse reads a Prometheus text exposition document.
//
// Malformed lines are skipped rather than failing the whole scrape: a partial
// set of metrics is more useful to a monitor than none.
func Parse(r io.Reader) (Set, error) {
	set := make(Set)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			parseMeta(set, line)
			continue
		}
		name, labels, value, ok := parseSample(line)
		if !ok {
			continue
		}
		f := family(set, name)
		f.Samples = append(f.Samples, Sample{Name: name, Labels: labels, Value: value})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

func family(set Set, name string) *Family {
	if f, ok := set[name]; ok {
		return f
	}
	f := &Family{Name: name}
	set[name] = f
	return f
}

// parseMeta handles "# HELP <name> <text>" and "# TYPE <name> <type>" lines.
func parseMeta(set Set, line string) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	kind, tail, ok := cut(rest)
	if !ok {
		return
	}
	name, text, _ := cut(tail)
	if name == "" {
		return
	}
	switch strings.ToUpper(kind) {
	case "HELP":
		family(set, name).Help = text
	case "TYPE":
		family(set, name).Type = strings.ToLower(strings.TrimSpace(text))
	}
}

// parseSample handles `name{k="v",...} 1.23` and `name 1.23`.
func parseSample(line string) (string, map[string]string, float64, bool) {
	var name, labelPart, valuePart string

	if open := strings.IndexByte(line, '{'); open >= 0 {
		end := strings.LastIndexByte(line, '}')
		if end < open {
			return "", nil, 0, false
		}
		name = strings.TrimSpace(line[:open])
		labelPart = line[open+1 : end]
		valuePart = strings.TrimSpace(line[end+1:])
	} else {
		n, v, ok := cut(line)
		if !ok {
			return "", nil, 0, false
		}
		name, valuePart = n, v
	}

	if name == "" {
		return "", nil, 0, false
	}

	// A trailing timestamp may follow the value; only the value is needed.
	if f, _, ok := cut(valuePart); ok {
		valuePart = f
	}
	value, err := parseFloat(valuePart)
	if err != nil {
		return "", nil, 0, false
	}
	return name, parseLabels(labelPart), value, true
}

// parseLabels splits a comma-separated label list, honouring quoted commas.
func parseLabels(s string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	labels := make(map[string]string)
	var buf strings.Builder
	inQuote, escaped := false, false

	flush := func() {
		pair := strings.TrimSpace(buf.String())
		buf.Reset()
		if pair == "" {
			return
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if unquoted, err := strconv.Unquote(v); err == nil {
			v = unquoted
		}
		if k != "" {
			labels[k] = v
		}
	}

	for _, r := range s {
		switch {
		case escaped:
			escaped = false
			buf.WriteRune(r)
		case r == '\\' && inQuote:
			escaped = true
			buf.WriteRune(r)
		case r == '"':
			inQuote = !inQuote
			buf.WriteRune(r)
		case r == ',' && !inQuote:
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	flush()

	if len(labels) == 0 {
		return nil
	}
	return labels
}

// parseFloat accepts the Prometheus spellings of non-finite values.
func parseFloat(s string) (float64, error) {
	switch strings.TrimSpace(s) {
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// cut splits on the first run of whitespace.
func cut(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, "", s != ""
	}
	return s[:i], strings.TrimSpace(s[i+1:]), true
}
