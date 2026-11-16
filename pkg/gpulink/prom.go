package gpulink

import (
	"bufio"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// promSample is one line of a Prometheus text exposition: a metric name, its
// labels and its value.
type promSample struct {
	name   string
	labels map[string]string
	value  float64
}

// promMetrics is a parsed exposition, samples grouped by metric name.
type promMetrics map[string][]promSample

// parseProm reads the Prometheus text format (the lines engines serve on
// /metrics). Comments and malformed lines are skipped: an engine's scrape is
// read for what it can tell, never refused as a whole.
func parseProm(r io.Reader) promMetrics {
	out := promMetrics{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, ok := parsePromLine(line)
		if ok {
			out[s.name] = append(out[s.name], s)
		}
	}
	return out
}

func parsePromLine(line string) (promSample, bool) {
	s := promSample{labels: map[string]string{}}
	i := strings.IndexAny(line, "{ ")
	if i <= 0 {
		return s, false
	}
	s.name = line[:i]
	rest := line[i:]
	if strings.HasPrefix(rest, "{") {
		end, ok := parsePromLabels(rest[1:], s.labels)
		if !ok {
			return s, false
		}
		rest = rest[1+end:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, false
	}
	v, err := parsePromValue(fields[0])
	if err != nil {
		return s, false
	}
	s.value = v
	return s, true
}

// parsePromLabels reads `k="v",...}` into labels and returns the index just
// past the closing brace.
func parsePromLabels(s string, labels map[string]string) (int, bool) {
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		if i < len(s) && s[i] == '}' {
			return i + 1, true
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			return 0, false
		}
		key := strings.TrimSpace(s[i : i+eq])
		i += eq + 1
		if i >= len(s) || s[i] != '"' {
			return 0, false
		}
		i++
		var val strings.Builder
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					val.WriteByte('\n')
				default:
					val.WriteByte(s[i])
				}
			} else {
				val.WriteByte(s[i])
			}
			i++
		}
		if i >= len(s) {
			return 0, false
		}
		i++ // closing quote
		labels[key] = val.String()
	}
	return 0, false
}

func parsePromValue(s string) (float64, error) {
	switch s {
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(s, 64)
}

// sum adds every sample of the first name present, so a counter split by
// label (per model, per outcome) reads as its total. ok is false when none of
// the names is present.
func (m promMetrics) sum(names ...string) (float64, bool) {
	for _, n := range names {
		if samples, ok := m[n]; ok && len(samples) > 0 {
			total := 0.0
			for _, s := range samples {
				if !math.IsNaN(s.value) {
					total += s.value
				}
			}
			return total, true
		}
	}
	return 0, false
}

// byLabel sums the first present name's samples per value of label.
func (m promMetrics) byLabel(label string, names ...string) map[string]float64 {
	for _, n := range names {
		if samples, ok := m[n]; ok && len(samples) > 0 {
			out := map[string]float64{}
			for _, s := range samples {
				out[s.labels[label]] += s.value
			}
			return out
		}
	}
	return nil
}

// label returns label's value on the first sample of the first present name.
func (m promMetrics) label(label string, names ...string) string {
	for _, n := range names {
		for _, s := range m[n] {
			if v := s.labels[label]; v != "" {
				return v
			}
		}
	}
	return ""
}

// histogram reads the first present histogram name (its _bucket, _sum and
// _count series), adding samples that differ only in other labels.
func (m promMetrics) histogram(names ...string) *Histogram {
	for _, n := range names {
		buckets, ok := m[n+"_bucket"]
		if !ok || len(buckets) == 0 {
			continue
		}
		byLE := map[float64]float64{}
		for _, b := range buckets {
			le, err := parsePromValue(b.labels["le"])
			if err != nil {
				continue
			}
			byLE[le] += b.value
		}
		h := &Histogram{}
		for le, c := range byLE {
			h.Buckets = append(h.Buckets, Bucket{LE: le, Count: c})
		}
		sort.Slice(h.Buckets, func(i, j int) bool { return h.Buckets[i].LE < h.Buckets[j].LE })
		h.Sum, _ = m.sum(n + "_sum")
		h.Count, _ = m.sum(n + "_count")
		return h
	}
	return nil
}
