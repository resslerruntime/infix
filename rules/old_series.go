package rules

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/oktal/infix/logging"

	"github.com/influxdata/influxdb/tsdb/engine/tsm1"
	"github.com/oktal/infix/storage"
)

type formater interface {
	format(iow io.Writer, serie string, timestamp int64) error
}

type textFormater struct {
	withTimestamp   bool
	timestampLayout string
}

func formatTimestamp(unixNano int64, layout string) string {
	if layout == "" {
		return string(unixNano)
	}

	ts := time.Unix(0, unixNano)
	if strings.EqualFold(layout, "RFC3339") {
		return ts.Format(time.RFC3339)
	}

	return ts.Format(layout)
}

func (f *textFormater) format(iow io.Writer, serie string, timestamp int64) error {
	if f.withTimestamp {
		fmt.Fprintf(iow, "%s: %s\n", serie, formatTimestamp(timestamp, f.timestampLayout))
	} else {
		fmt.Fprintf(iow, "%s\n", serie)
	}
	return nil
}

type jsonFormater struct {
	withTimestamp   bool
	timestampLayout string
}

func (f *jsonFormater) format(iow io.Writer, serie string, timestamp int64) error {
	type jsonLine struct {
		serie     string
		timestamp int64
	}
	type jsonLineSerieOnly struct {
		serie string
	}

	data := map[string]interface{}{
		"Serie": serie,
	}

	if f.withTimestamp {
		data["Timestamp"] = formatTimestamp(timestamp, f.timestampLayout)
	}
	return f.formatLine(iow, data)
}

func (f *jsonFormater) formatLine(iow io.Writer, data map[string]interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintln(iow, string(b))
	return nil
}

// OldSeriesRule defines a read-only rule to retrieve series that are oldest than a given timestamp
type OldSeriesRule struct {
	unixNano int64
	out      io.Writer

	series   map[string]int64
	formater formater

	logger *log.Logger
}

// OldSerieRuleConfig represents the toml configuration for OldSerieRule
type OldSerieRuleConfig struct {
	Time            string
	Out             string
	Format          string
	Timestamp       bool
	TimestampLayout string
}

func newFormater(format string, withTimestamp bool, timestampLayout string) (formater, error) {
	switch format {
	case "text":
		return &textFormater{withTimestamp: withTimestamp, timestampLayout: timestampLayout}, nil
	case "json":
		return &jsonFormater{withTimestamp: withTimestamp, timestampLayout: timestampLayout}, nil
	default:
		return nil, fmt.Errorf("Unknown format %s", format)
	}
}

// NewOldSeriesRule creates a new OldSeriesRule
func NewOldSeriesRule(t time.Time, out io.Writer, format string) (*OldSeriesRule, error) {
	formater, err := newFormater(format, false, "")
	if err != nil {
		return nil, err
	}

	return newOldSeriesRule(t, out, formater), nil
}

func newOldSeriesRule(t time.Time, out io.Writer, formater formater) *OldSeriesRule {
	return &OldSeriesRule{
		unixNano: t.UnixNano() / int64(time.Nanosecond),
		out:      out,
		series:   make(map[string]int64),
		formater: formater,
		logger:   logging.GetLogger("OldSeriesRule"),
	}
}

// CheckMode sets the check mode on the rule
func (r *OldSeriesRule) CheckMode(check bool) {

}

// Flags implements Rule interface
func (r *OldSeriesRule) Flags() int {
	return TSMReadOnly
}

// WithLogger sets the logger on the rule
func (r *OldSeriesRule) WithLogger(logger *log.Logger) {

}

// Start implements Rule interface
func (r *OldSeriesRule) Start() {

}

// End implements Rule interface
func (r *OldSeriesRule) End() {
	var keys []string
	for k := range r.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	count := 0

	for _, key := range keys {
		maxTs := r.series[key]
		if maxTs <= r.unixNano {
			r.formater.format(r.out, key, maxTs)
			count++
		}
	}
	r.logger.Printf("Detected %d/%d series as old", count, len(keys))
}

// StartShard implements Rule interface
func (r *OldSeriesRule) StartShard(info storage.ShardInfo) {

}

// EndShard implements Rule interface
func (r *OldSeriesRule) EndShard() error {
	return nil
}

// StartTSM implements Rule interface
func (r *OldSeriesRule) StartTSM(path string) {

}

// EndTSM implements Rule interface
func (r *OldSeriesRule) EndTSM() {

}

// StartWAL implements Rule interface
func (r *OldSeriesRule) StartWAL(path string) {

}

// EndWAL implements Rule interface
func (r *OldSeriesRule) EndWAL() {

}

// Apply implements Rule interface
func (r *OldSeriesRule) Apply(key []byte, values []tsm1.Value) ([]byte, []tsm1.Value, error) {
	seriesKey, _ := tsm1.SeriesAndFieldFromCompositeKey(key)
	maxTs := values[len(values)-1].UnixNano()

	s := string(seriesKey)
	if ts, ok := r.series[s]; ok {
		if maxTs > ts {
			r.series[s] = maxTs
		}
	} else {
		r.series[s] = maxTs
	}

	return nil, nil, nil
}

// Print will print the list of series detected as old
func (r *OldSeriesRule) Print(iow io.Writer) {
}

// Sample implements Config interface
func (c *OldSerieRuleConfig) Sample() string {
	return `
	[[rules.old-serie]]
		time="2020-01-01 00:08:00"
		out=stdout
		# out=out_file.log
		format=text
		timestamp=true
		# format=json
	`
}

// Build implements Config interface
func (c *OldSerieRuleConfig) Build() (Rule, error) {
	t, err := time.Parse(time.RFC3339, c.Time)
	if err != nil {
		return nil, err
	}

	var out io.Writer
	if c.Out == "" {
		out = os.Stdout
	} else if c.Out == "stdout" {
		out = os.Stdout
	} else if c.Out == "stderr" {
		out = os.Stderr
	} else {
		out, err = os.Create(c.Out)
		if err != nil {
			return nil, err
		}
	}

	format := "text"
	if c.Format != "" {
		format = c.Format
	}

	formater, err := newFormater(format, c.Timestamp, c.TimestampLayout)
	if err != nil {
		return nil, err
	}

	return newOldSeriesRule(t, out, formater), nil
}
