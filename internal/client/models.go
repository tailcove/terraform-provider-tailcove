package client

import (
	"context"
	"net/http"
)

// Source is a log source. IngestToken is set only in the response to a create.
type Source struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Platform      *string `json:"platform"`
	RetentionDays int64   `json:"retentionDays"`
	ClientGroupID *int64  `json:"clientGroupId"`
	IngestToken   string  `json:"ingestToken"`
}

// Header is one request header of an HTTP monitor.
type Header struct {
	Name string `json:"name"`
}

// Monitor is an uptime monitor of any type; a field its type does not use is null.
type Monitor struct {
	ID                 int64          `json:"id"`
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	URL                *string        `json:"url"`
	Host               *string        `json:"host"`
	Port               *int64         `json:"port"`
	Method             *string        `json:"method"`
	ExpectedStatus     *int64         `json:"expectedStatus"`
	Keyword            *string        `json:"keyword"`
	DNSRecordType      *string        `json:"dnsRecordType"`
	DNSExpected        *string        `json:"dnsExpected"`
	WarnDays           *int64         `json:"warnDays"`
	Regions            []string       `json:"regions"`
	MinRegionsDown     *int64         `json:"minRegionsDown"`
	RequestBody        *string        `json:"requestBody"`
	IntervalSeconds    *int64         `json:"intervalSeconds"`
	GraceSeconds       *int64         `json:"graceSeconds"`
	PingURL            *string        `json:"pingUrl"`
	TimeoutMs          *int64         `json:"timeoutMs"`
	IsActive           bool           `json:"isActive"`
	CreateIncident     bool           `json:"createIncident"`
	EscalationPolicyID *int64         `json:"escalationPolicyId"`
	State              string         `json:"state"`
	Channels           map[string]any `json:"channels"`
	CreatedAt          *string        `json:"createdAt"`
	ClientGroupID      *int64         `json:"clientGroupId"`
}

// Alert is an alert of a source.
type Alert struct {
	ID                 int64          `json:"id"`
	SourceID           int64          `json:"sourceId"`
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	MetricName         *string        `json:"metricName"`
	MetricHost         *string        `json:"metricHost"`
	MetricAggregate    *string        `json:"metricAggregate"`
	MetricOperator     *string        `json:"metricOperator"`
	MetricThreshold    *float64       `json:"metricThreshold"`
	Query              *string        `json:"query"`
	NoData             bool           `json:"noData"`
	AnomalyMultiple    *float64       `json:"anomalyMultiple"`
	ThresholdCount     *int64         `json:"thresholdCount"`
	WindowMinutes      int64          `json:"windowMinutes"`
	CooldownMinutes    int64          `json:"cooldownMinutes"`
	Channels           map[string]any `json:"channels"`
	IsActive           bool           `json:"isActive"`
	CreateIncident     bool           `json:"createIncident"`
	EscalationPolicyID *int64         `json:"escalationPolicyId"`
	CreatedAt          *string        `json:"createdAt"`
}

// EscalationPolicy is read-only here: the API lists policies, the web app edits them.
type EscalationPolicy struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Steps       int64  `json:"steps"`
	RepeatCount int64  `json:"repeatCount"`
}

// Schedule is an on-call schedule (read-only here).
type Schedule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	TimeZone string `json:"timeZone"`
}

func (c *Client) ListSources(ctx context.Context) ([]Source, error) {
	return get[[]Source](ctx, c, "/sources")
}

func (c *Client) GetSource(ctx context.Context, id int64) (*Source, error) {
	s, err := get[Source](ctx, c, "/sources/"+idPath(id))
	return &s, err
}

func (c *Client) CreateSource(ctx context.Context, body map[string]any) (*Source, error) {
	s, err := send[Source](ctx, c, http.MethodPost, "/sources", body)
	return &s, err
}

func (c *Client) UpdateSource(ctx context.Context, id int64, body map[string]any) (*Source, error) {
	s, err := send[Source](ctx, c, http.MethodPut, "/sources/"+idPath(id), body)
	return &s, err
}

func (c *Client) DeleteSource(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, "/sources/"+idPath(id), nil, nil)
}

func (c *Client) GetMonitor(ctx context.Context, id int64) (*Monitor, error) {
	m, err := get[Monitor](ctx, c, "/monitors/"+idPath(id))
	return &m, err
}

func (c *Client) CreateMonitor(ctx context.Context, body map[string]any) (*Monitor, error) {
	m, err := send[Monitor](ctx, c, http.MethodPost, "/monitors", body)
	return &m, err
}

func (c *Client) UpdateMonitor(ctx context.Context, id int64, body map[string]any) (*Monitor, error) {
	m, err := send[Monitor](ctx, c, http.MethodPut, "/monitors/"+idPath(id), body)
	return &m, err
}

func (c *Client) DeleteMonitor(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, "/monitors/"+idPath(id), nil, nil)
}

func (c *Client) GetAlert(ctx context.Context, sourceID, id int64) (*Alert, error) {
	a, err := get[Alert](ctx, c, "/sources/"+idPath(sourceID)+"/alerts/"+idPath(id))
	return &a, err
}

func (c *Client) CreateAlert(ctx context.Context, sourceID int64, body map[string]any) (*Alert, error) {
	a, err := send[Alert](ctx, c, http.MethodPost, "/sources/"+idPath(sourceID)+"/alerts", body)
	return &a, err
}

func (c *Client) UpdateAlert(ctx context.Context, sourceID, id int64, body map[string]any) (*Alert, error) {
	a, err := send[Alert](ctx, c, http.MethodPut, "/sources/"+idPath(sourceID)+"/alerts/"+idPath(id), body)
	return &a, err
}

func (c *Client) DeleteAlert(ctx context.Context, sourceID, id int64) error {
	return c.do(ctx, http.MethodDelete, "/sources/"+idPath(sourceID)+"/alerts/"+idPath(id), nil, nil)
}

func (c *Client) ListEscalationPolicies(ctx context.Context) ([]EscalationPolicy, error) {
	return get[[]EscalationPolicy](ctx, c, "/oncall/policies")
}

func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return get[[]Schedule](ctx, c, "/oncall/schedules")
}

func idPath(n int64) string { return id(n) }
