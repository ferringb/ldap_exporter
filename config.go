package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/prometheus/common/log"

	"gopkg.in/ldap.v2"
	"gopkg.in/yaml.v2"
)

var (
	defaultCounterNameTemplate *templateString
	defaultmetricNameTemplate  *templateString
)

func init() {
	defaultmetricNameTemplate = &templateString{
		template: template.Must(
			template.New("default").Parse(
				"{{ .section }}_{{ .attribute }}",
			),
		),
	}
	defaultCounterNameTemplate = &templateString{
		template: template.Must(
			template.New("default").Parse(
				"{{ .section }}_{{ .attribute }}_total",
			),
		),
	}
}

type dnString string

func (d *dnString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if _, err := ldap.ParseDN(s); err != nil {
		return fmt.Errorf("search is malformed: %s", err)
	}
	*d = (dnString)(s)
	return nil
}

type filterString string

func (f *filterString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if _, err := ldap.CompileFilter(s); err != nil {
		return fmt.Errorf("filter is malformed: %s", err)
	}
	*f = (filterString)(s)
	return nil
}

type templateString struct {
	template *template.Template
}

func (t *templateString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	new_t, err := template.New("config supplied template").Funcs((template.FuncMap)(sprig.FuncMap())).Parse(s)
	if err != nil {
		return fmt.Errorf("template parse failure; error was %s, template was:\n%s", err, s)
	}
	t.template = new_t
	return nil
}

type metricAttributeConfig struct {
	Name       string         `yaml:"metric_name"`
	Type       string         `yaml:"type"`
	Labels     []string       `yaml:"labels"`
	Translator templateString `yaml:"translator"`
	Help       string         `yaml:"help"`

	X map[string]interface{} `yaml:",inline"`
}

func (mac *metricAttributeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain metricAttributeConfig

	if err := unmarshal((*plain)(mac)); err != nil {
		return err
	}

	if err := checkOverflow(mac.X, "config"); err != nil {
		return err
	}

	if mac.Type == "" {
		return fmt.Errorf("type must be defined")
	}

	for idx, label := range mac.Labels {
		if len(strings.TrimSpace(label)) != len(label) {
			return fmt.Errorf("label at index %d cannot have whitespace and must be nonempty: '%s'", idx, label)
		}
	}

	return nil
}

type attributeConfig struct {
	Labels  map[string]string                `yaml:"labels"`
	Metrics map[string]metricAttributeConfig `yaml:"metrics"`

	X map[string]interface{} `yaml:",inline"`
}

func (a *attributeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain attributeConfig

	if err := unmarshal((*plain)(a)); err != nil {
		return err
	}

	if err := checkOverflow(a.X, "config"); err != nil {
		return err
	}
	return nil
}

type scopeChoice int

func (s *scopeChoice) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var choice string
	if err := unmarshal(&choice); err != nil {
		return err
	}

	// support the gopkg/ldap.v2 names for this, in addition to our shortened names.
	for scope, name := range ldap.ScopeMap {
		if name == choice {
			*s = scopeChoice(scope)
			return nil
		}
	}
	switch choice {
	case "base":
		*s = ldap.ScopeBaseObject
		return nil
	case "single":
		*s = ldap.ScopeSingleLevel
		return nil
	case "subtree":
		*s = ldap.ScopeWholeSubtree
		return nil
	}
	return fmt.Errorf("ldap search scope %s is unknown; supported options are 'base', 'single', and 'subtree'.  Optionally, you can also use the ldap.v2's naming: %s", choice, ldap.ScopeMap)
}

type derefChoice int

func (d *derefChoice) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var choice string
	if err := unmarshal(&choice); err != nil {
		return err
	}

	switch choice {
	case "never":
		*d = ldap.NeverDerefAliases
		return nil
	case "search":
		*d = ldap.DerefInSearching
		return nil
	case "base":
		*d = ldap.DerefFindingBaseObj
		return nil
	case "always":
		*d = ldap.DerefAlways
		return nil
	}
	return fmt.Errorf("ldap deref choice %s is unknown; supported options are 'never', 'search', 'base', and 'always'", choice)
}

type metricSourceConfig struct {
	Name   string        `yaml:"name"`
	Search *dnString     `yaml:"search"`
	Filter *filterString `yaml:"filter"`
	Scope  *scopeChoice  `yaml:"scope"`
	Deref  *derefChoice  `yaml:"deref"`

	CounterNameTemplate *templateString   `yaml:"counter_metric_name_template"`
	GaugeNameTemplate   *templateString   `yaml:"gauge_metric_name_template"`
	Attributes          attributeConfig   `yaml:"attributes"`
	ConstantLabels      map[string]string `yaml:"labels"`

	labelsFromAttributes []string
	metricAttributes     map[string]MetricAttribute

	X map[string]interface{} `yaml:",inline"`
}

func (s *metricSourceConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {

	type plain metricSourceConfig

	if err := unmarshal((*plain)(s)); err != nil {
		return err
	}

	if err := checkOverflow(s.X, "config"); err != nil {
		return err
	}

	if s.Search == nil {
		return fmt.Errorf("search is either empty or undefined")
	}
	if s.Filter == nil {
		var f = "(objectClass=*)"
		s.Filter = (*filterString)(&f)
	}
	if s.CounterNameTemplate == nil {
		s.CounterNameTemplate = defaultCounterNameTemplate
	}
	if s.GaugeNameTemplate == nil {
		s.GaugeNameTemplate = defaultmetricNameTemplate
	}

	if s.Scope == nil {
		// go doesn't let you take the address of a raw integer- say ldap.ScopeBaseObject for example.
		// hence the new shenanigans here so we have an object.
		s.Scope = new(scopeChoice)
		*s.Scope = ldap.ScopeBaseObject
	}
	if s.Deref == nil {
		// go doesn't let you take the address of a raw integer- say ldap.ScopeBaseObject for example.
		// hence the new shenanigans here so we have an object.
		s.Deref = new(derefChoice)
		*s.Deref = ldap.DerefAlways
	}

	for key, value := range s.ConstantLabels {
		if len(strings.TrimSpace(value)) != len(value) {
			return fmt.Errorf("constant label for attribute %s cannot have whitespace and must be nonempty: '%s'", key, value)
		}
	}

	s.metricAttributes = make(map[string]MetricAttribute)
	for src, final_name := range s.Attributes.Labels {
		for _, v := range s.labelsFromAttributes {
			if final_name == v {
				return fmt.Errorf("duplicate label names found for %s->%s; '%s' already is a label", src, final_name, final_name)
			}
		}
		s.labelsFromAttributes = append(s.labelsFromAttributes, final_name)
	}
	for attr, metric_config := range s.Attributes.Metrics {
		if err := checkOverflow(metric_config.X, fmt.Sprintf("attribute %s", attr)); err != nil {
			return err
		}
		if err := s.createMetricAttribute(&metric_config, attr); err != nil {
			return err
		}
	}

	return nil
}

func (msc *metricSourceConfig) createMetricAttribute(a *metricAttributeConfig, attribute string) error {
	help := a.Help
	if help == "" {
		log.Warnf("section %s, attribute %s: no help provided", msc.Name, attribute)
		help = "No help provided"
	}

	setName := func(t *templateString) error {
		if a.Name == "" {
			var buffer bytes.Buffer
			if err := t.template.Option("missingkey=error").Execute(&buffer, map[string]string{"section": msc.Name, "attribute": attribute}); err != nil {
				return fmt.Errorf("attribute %s naming error: %s", attribute, err)
			}
			log.Debugf("templating metric name for attr %s to %s", attribute, buffer.String())
			a.Name = buffer.String()
		}
		a.Name = fmt.Sprintf("ldap_%s", a.Name)
		return nil
	}

	labels := a.Labels
	if len(msc.labelsFromAttributes) != 0 {
		labels = []string{}
		labels = append(labels, msc.labelsFromAttributes...)
		labels = append(labels, a.Labels...)
	}
	switch a.Type {
	case "counter":
		if err := setName(msc.CounterNameTemplate); err != nil {
			return err
		}
		msc.metricAttributes[attribute] = (MetricAttribute)(NewCounterMetricAttribute(
			a.Name,
			labels,
			msc.ConstantLabels,
			a.Translator.template,
			help,
		))
	case "gauge":
		if err := setName(msc.GaugeNameTemplate); err != nil {
			return err
		}
		msc.metricAttributes[attribute] = (MetricAttribute)(NewGaugeMetricAttribute(
			a.Name,
			labels,
			msc.ConstantLabels,
			a.Translator.template,
			help,
		))
	default:
		return fmt.Errorf("type %s isn't valid for attribute %s", a.Type, attribute)
	}
	return nil
}

func LoadConfig(data string) ([]*MetricsSource, error) {
	var parsed_data []metricSourceConfig
	if err := yaml.Unmarshal([]byte(data), &parsed_data); err != nil {
		return nil, err
	}

	var sources []*MetricsSource

	for _, section := range parsed_data {
		sources = append(sources, NewMetricsSource((*string)(section.Search), (*string)(section.Filter), (int)(*section.Scope), (int)(*section.Deref), section.metricAttributes, section.Attributes.Labels))
	}
	return sources, nil
}

func LoadConfigFile(path string) ([]*MetricsSource, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadConfig(string(content))
}
