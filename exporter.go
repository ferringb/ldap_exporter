package main

import (
	"bytes"
	"fmt"
	"strconv"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"gopkg.in/ldap.v2"
	"gopkg.in/yaml.v2"
)

const namespace = "ldap"

type MetricAttribute interface {
	Parse(map[string]string, *ldap.EntryAttribute) ([]prometheus.Metric, error)
	GetDesc() *prometheus.Desc
}

type CounterMetricAttribute struct {
	Desc       *prometheus.Desc
	labels     []string
	translator *template.Template
}

func NewCounterMetricAttribute(metric_name string, labels []string, constant_labels map[string]string, translator *template.Template, help string) *CounterMetricAttribute {
	return &CounterMetricAttribute{
		translator: translator,
		labels:     labels,
		Desc: prometheus.NewDesc(
			metric_name,
			help,
			labels,
			prometheus.Labels(constant_labels),
		),
	}
}

type translationResult struct {
	Value  float64           `yaml:"value"`
	Labels map[string]string `yaml:"labels"`

	X map[string]interface{} `yaml:",inline"`
}

func do_the_translation_thing(t *template.Template, values []string) ([]*translationResult, error) {
	var buffer bytes.Buffer
	if err := t.Option("missingkey=error").Execute(&buffer, map[string]interface{}{"values": values, "value": values[0]}); err != nil {
		return nil, fmt.Errorf("failed parsing for value %s: error was %s", values, err)
	}
	var results [](*translationResult)
	// yay, got back something that is hopefully yaml
	if err := yaml.Unmarshal([]byte(buffer.String()), &results); err != nil {
		return nil, fmt.Errorf("failed parsing value %s due to %s; intermediate yaml was:\n%s", values, err, buffer.String())
	}
	for idx, result := range results {
		if err := checkOverflow(result.X, "interpretting ldap->yaml translation"); err != nil {
			return nil, fmt.Errorf("failed parsing value %s: had unknown field in position %d: %s", values, idx, err)
		}
	}
	return results, nil
}

func buildOrderedLabels(desc_labels []string, label_sources ...map[string]string) ([]string, error) {
	results := make([]string, len(desc_labels))
	for result_idx, label_name := range desc_labels {
		found := false
		for _, source := range label_sources {
			if value, ok := source[label_name]; ok {
				found = true
				results[result_idx] = value
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("label %s wasn't found in label sources %s", label_name, label_sources)
		}
	}
	return results, nil
}

func (c *CounterMetricAttribute) Parse(extra_labels map[string]string, entry *ldap.EntryAttribute) ([]prometheus.Metric, error) {
	if c.translator == nil {
		if len(entry.Values) != 1 {
			return nil, fmt.Errorf("Attribute %s resulted in %d matches, but no translator was defined to convert this into labeled counts", entry.Name, len(entry.Values))
		}
		x, err := strconv.ParseUint(entry.Values[0], 10, 64)
		if err != nil {
			return nil, err
		}
		labels, err := buildOrderedLabels(c.labels, extra_labels)
		if err != nil {
			return nil, err
		}
		metric, err := prometheus.NewConstMetric(c.Desc, prometheus.CounterValue, float64(x), labels...)
		if err != nil {
			return nil, err
		}
		return []prometheus.Metric{metric}, nil
	}
	values, err := do_the_translation_thing(c.translator, entry.Values)
	if err != nil {
		return nil, err
	}
	var metrics []prometheus.Metric
	for _, value := range values {
		labels, err := buildOrderedLabels(c.labels, value.Labels, extra_labels)
		if err != nil {
			return nil, err
		}
		metric, err := prometheus.NewConstMetric(c.Desc, prometheus.CounterValue, value.Value, labels...)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

func (c *CounterMetricAttribute) GetDesc() *prometheus.Desc {
	return c.Desc
}

type GaugeMetricAttribute struct {
	Desc       *prometheus.Desc
	labels     []string
	translator *template.Template
}

func NewGaugeMetricAttribute(metric_name string, labels []string, constant_labels map[string]string, translator *template.Template, help string) *GaugeMetricAttribute {
	return &GaugeMetricAttribute{
		translator: translator,
		labels:     labels,
		Desc: prometheus.NewDesc(
			metric_name,
			help,
			labels,
			prometheus.Labels(constant_labels),
		),
	}
}

func (g *GaugeMetricAttribute) Parse(extra_labels map[string]string, entry *ldap.EntryAttribute) ([]prometheus.Metric, error) {
	var metrics []prometheus.Metric
	if g.translator == nil {
		if len(entry.Values) != 1 {
			return nil, fmt.Errorf("Attribute %s resulted in %d matches, but no translator was defined to convert this into labeled counts", entry.Name, len(entry.Values))
		}
		x, err := strconv.ParseFloat(entry.Values[0], 64)
		if err != nil {
			return nil, err
		}
		labels, err := buildOrderedLabels(g.labels, extra_labels)
		if err != nil {
			return nil, err
		}
		metric, err := prometheus.NewConstMetric(g.Desc, prometheus.CounterValue, float64(x), labels...)
		if err != nil {
			return nil, fmt.Errorf("Failed creating metric %s: %s", g.Desc, err)
		}
		return []prometheus.Metric{metric}, nil
	}
	values, err := do_the_translation_thing(g.translator, entry.Values)
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		labels, err := buildOrderedLabels(g.labels, value.Labels, extra_labels)
		if err != nil {
			return nil, err
		}
		metric, err := prometheus.NewConstMetric(g.Desc, prometheus.GaugeValue, value.Value, labels...)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

func (g *GaugeMetricAttribute) GetDesc() *prometheus.Desc {
	return g.Desc
}

type MetricsSource struct {
	SearchRequest    *ldap.SearchRequest
	MetricAttributes map[string]MetricAttribute
	LabelAttributes  map[string]string
}

func NewMetricsSource(searchDN *string, filter *string, scope int, deref int, metric_attributes map[string]MetricAttribute, label_attributes map[string]string) *MetricsSource {
	var attrs []string
	for attr := range metric_attributes {
		attrs = append(attrs, attr)
	}
	for attr, _ := range label_attributes {
		attrs = append(attrs, attr)
	}
	if filter == nil {
		s := "(objectClass=*)"
		filter = &s
	}
	search := ldap.NewSearchRequest(
		*searchDN,
		scope, deref, 0, 0, false,
		*filter,
		attrs,
		nil,
	)
	m := MetricsSource{
		SearchRequest:    search,
		MetricAttributes: metric_attributes,
		LabelAttributes:  label_attributes,
	}
	return &m
}

func (m *MetricsSource) String() string {
	return fmt.Sprintf("search='%v', filter: '%v'", m.SearchRequest.BaseDN, m.SearchRequest.Filter)
}

type Exporter struct {
	duration     prometheus.Gauge
	scrapeError  prometheus.Gauge
	totalErrors  prometheus.Counter
	totalScrapes prometheus.Counter

	conn           *ldap.Conn
	metricsSources []*MetricsSource
}

func NewExporter(conn *ldap.Conn, sources []*MetricsSource) *Exporter {
	return &Exporter{
		conn:           conn,
		metricsSources: sources,
		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from LDAP.",
		}),
		scrapeError: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "last_scrape_error",
			Help:      "Count of individual LDAP queries in the last scrape that failed.  Zero is success, anything else is failures.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "scrapes_total",
			Help:      "Total number of times LDAP was scraped for metrics.",
		}),
		totalErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "errors_total",
			Help:      "Total number of times the exporter experienced errors collecting LDAP metrics.",
		}),
	}

}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	log.Debug("describing metrics")
	for _, query := range e.metricsSources {
		for _, attr := range query.MetricAttributes {
			ch <- attr.GetDesc()
		}
	}
}

func (m *MetricsSource) scrapeMetrics(result *ldap.SearchResult, ch chan<- prometheus.Metric) error {
	for _, e := range result.Entries {
		labels := make(map[string]string)
		// first collect all attributes that are labels
		for _, attribute := range e.Attributes {
			if remapped_label_name, ok := m.LabelAttributes[attribute.Name]; ok {
				if len(attribute.Values) != 1 {
					return fmt.Errorf("attribute %s is a label type but has multiple values: %s", attribute.Name, attribute.Values)
				}
				labels[remapped_label_name] = attribute.Values[0]
			}
		}
		if len(labels) != len(m.LabelAttributes) {
			// any metrics we generate will be rejected by prometheus due to label cardinality fail out.
			return fmt.Errorf("required label attributes weren't found, thus metrics can't be exported for this query.  Attribute->label name mapping was %s, only built %s", m.LabelAttributes, labels)
		}
		for _, attribute := range e.Attributes {
			metricVec, ok := m.MetricAttributes[attribute.Name]
			if !ok {
				if _, ok := m.LabelAttributes[attribute.Name]; !ok {
					return fmt.Errorf("server sent us an attribute we do not recognize (%s); this is likely a bug in the exporter", attribute.Name)
				}
				continue
			}
			metrics, err := metricVec.Parse(labels, attribute)
			if err != nil {
				return fmt.Errorf("while scraping %v: %s", m, err)
			}
			for _, metric := range metrics {
				ch <- metric
			}
		}
	}
	return nil
}

func (e *Exporter) scrape(ch chan<- prometheus.Metric) {
	e.totalScrapes.Inc()

	failures := float64(0)
	defer func(begin time.Time) {
		e.duration.Set(time.Since(begin).Seconds())
		e.scrapeError.Set(failures)
		e.totalErrors.Add(failures)
	}(time.Now())

	for _, source := range e.metricsSources {
		result, err := e.conn.Search(source.SearchRequest)
		if err != nil {
			log.Errorf("failed scraping for %v; Error was: %s", source, err)
			failures += 1
			continue
		}
		err = source.scrapeMetrics(result, ch)
		if err != nil {
			log.Errorf("failed scraping for %v; Error was: %s", source, err)
			failures += 1
		}
	}

}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	log.Debug("collecting metrics")

	e.scrape(ch)

	ch <- e.duration
	ch <- e.totalScrapes
	ch <- e.totalErrors
	ch <- e.scrapeError
}
