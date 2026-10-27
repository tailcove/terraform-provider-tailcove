package provider_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is a small in-memory stand-in for the Tailcove /api/v1 write API. It keeps what the real API
// does that matters to the provider: ids, camelCase responses, the ingest token only in a create's response,
// secrets that never come back, defaults, 404 for what is gone and a 422 for a bad request.
type fakeAPI struct {
	t  *testing.T
	mu sync.Mutex

	nextID   int64
	sources  map[int64]map[string]any
	monitors map[int64]map[string]any
	alerts   map[int64]map[string]any // by alert id; the source is in the body
	keys     map[string]bool

	// requests records "METHOD path" in order, so a test can see what the provider sent.
	requests []string
	// bodies keeps the last request body per "METHOD path".
	bodies map[string]map[string]any

	policies  []map[string]any
	schedules []map[string]any
}

func newFakeAPI(t *testing.T) (*fakeAPI, string) {
	f := &fakeAPI{
		t: t, nextID: 100,
		sources: map[int64]map[string]any{}, monitors: map[int64]map[string]any{}, alerts: map[int64]map[string]any{},
		keys: map[string]bool{}, bodies: map[string]map[string]any{},
		policies:  []map[string]any{{"id": 5, "name": "Primary on-call", "steps": 2, "repeatCount": 1}},
		schedules: []map[string]any{{"id": 8, "name": "Weekdays", "timeZone": "Europe/Berlin"}},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

var camelRe = regexp.MustCompile(`_([a-z])`)

func camel(s string) string {
	return camelRe.ReplaceAllStringFunc(s, func(m string) string { return strings.ToUpper(m[1:]) })
}

var secretFields = map[string]string{"github": "token", "telegram": "bot_token", "pagerduty": "routing_key", "opsgenie": "api_key"}

func maskChannels(in any) any {
	channels, ok := in.(map[string]any)
	if !ok {
		return in
	}
	out := map[string]any{}
	for name, v := range channels {
		cfg, _ := v.(map[string]any)
		copyCfg := map[string]any{}
		for k, val := range cfg {
			if k == secretFields[name] {
				copyCfg[k+"_set"] = true
				continue
			}
			copyCfg[k] = val
		}
		out[name] = copyCfg
	}
	return out
}

func present(id int64, body map[string]any, defaults map[string]any) map[string]any {
	out := map[string]any{"id": id}
	for k, v := range defaults {
		out[camel(k)] = v
	}
	for k, v := range body {
		switch k {
		case "channels":
			out["channels"] = maskChannels(v)
		case "request_headers":
			names := []map[string]string{}
			if list, ok := v.([]any); ok {
				for _, h := range list {
					names = append(names, map[string]string{"name": h.(map[string]any)["name"].(string), "value": ""})
				}
			}
			out["requestHeaders"] = names
		default:
			out[camel(k)] = v
		}
	}
	return out
}

func (f *fakeAPI) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (f *fakeAPI) invalid(w http.ResponseWriter, field, msg string) {
	f.writeJSON(w, 422, map[string]any{"message": "The given data was invalid.", "errors": map[string][]string{field: {msg}}})
}

var routeRe = regexp.MustCompile(`^/api/v1/(sources|monitors|oncall)(?:/(\d+|policies|schedules))?(?:/(alerts))?(?:/(\d+))?$`)

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer test-token" {
		f.writeJSON(w, 401, map[string]any{"message": "Unauthenticated."})
		return
	}
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	var body map[string]any
	if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodies[r.Method+" "+r.URL.Path] = body
	}
	if r.Method == http.MethodPost {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			f.t.Errorf("POST %s without an Idempotency-Key", r.URL.Path)
		}
		f.keys[key] = true
	}

	m := routeRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		f.writeJSON(w, 404, nil)
		return
	}
	resource, seg, alerts, alertSeg := m[1], m[2], m[3], m[4]
	id, _ := strconv.ParseInt(seg, 10, 64)

	switch {
	case resource == "oncall" && seg == "policies":
		f.writeJSON(w, 200, map[string]any{"data": f.policies})
	case resource == "oncall" && seg == "schedules":
		f.writeJSON(w, 200, map[string]any{"data": f.schedules})
	case resource == "sources" && alerts != "":
		f.alertRoute(w, r, id, alertSeg, body)
	case resource == "sources":
		f.sourceRoute(w, r, seg, id, body)
	case resource == "monitors":
		f.monitorRoute(w, r, seg, id, body)
	default:
		f.writeJSON(w, 404, nil)
	}
}

func (f *fakeAPI) sourceRoute(w http.ResponseWriter, r *http.Request, seg string, id int64, body map[string]any) {
	switch {
	case r.Method == http.MethodGet && seg == "":
		list := []any{}
		for sid, s := range f.sources {
			list = append(list, present(sid, s, map[string]any{"retention_days": 3}))
		}
		f.writeJSON(w, 200, map[string]any{"data": list})
	case r.Method == http.MethodPost && seg == "":
		if name, _ := body["name"].(string); name == "" {
			f.invalid(w, "name", "The name field is required.")
			return
		}
		f.nextID++
		if body["retention_days"] == nil {
			body["retention_days"] = 3
		}
		f.sources[f.nextID] = body
		out := present(f.nextID, body, nil)
		out["ingestToken"] = "tok_" + strconv.FormatInt(f.nextID, 10)
		f.writeJSON(w, 201, map[string]any{"data": out})
	case seg != "":
		s, ok := f.sources[id]
		if !ok {
			f.writeJSON(w, 404, nil)
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.writeJSON(w, 200, map[string]any{"data": present(id, s, nil)})
		case http.MethodPut:
			if body["retention_days"] == nil {
				body["retention_days"] = s["retention_days"]
			}
			f.sources[id] = body
			f.writeJSON(w, 200, map[string]any{"data": present(id, body, nil)})
		case http.MethodDelete:
			delete(f.sources, id)
			f.writeJSON(w, 204, nil)
		}
	default:
		f.writeJSON(w, 404, nil)
	}
}

func (f *fakeAPI) monitorRoute(w http.ResponseWriter, r *http.Request, seg string, id int64, body map[string]any) {
	defaults := map[string]any{"is_active": true, "create_incident": false, "state": "unknown"}
	switch {
	case r.Method == http.MethodPost && seg == "":
		if t, _ := body["type"].(string); (t == "" || t == "http") && body["url"] == nil {
			f.invalid(w, "url", "The url field is required.")
			return
		}
		f.nextID++
		f.monitors[f.nextID] = body
		f.writeJSON(w, 201, map[string]any{"data": present(f.nextID, body, defaults)})
	case seg != "":
		mon, ok := f.monitors[id]
		if !ok {
			f.writeJSON(w, 404, nil)
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.writeJSON(w, 200, map[string]any{"data": present(id, mon, defaults)})
		case http.MethodPut:
			f.monitors[id] = body
			f.writeJSON(w, 200, map[string]any{"data": present(id, body, defaults)})
		case http.MethodDelete:
			delete(f.monitors, id)
			f.writeJSON(w, 204, nil)
		}
	default:
		f.writeJSON(w, 404, nil)
	}
}

func (f *fakeAPI) alertRoute(w http.ResponseWriter, r *http.Request, sourceID int64, seg string, body map[string]any) {
	if _, ok := f.sources[sourceID]; !ok {
		f.writeJSON(w, 404, nil)
		return
	}
	defaults := func(sid int64) map[string]any {
		return map[string]any{"source_id": sid, "type": "log", "no_data": false, "is_active": true, "create_incident": false}
	}
	switch {
	case r.Method == http.MethodPost && seg == "":
		f.nextID++
		body["source_id"] = sourceID
		f.alerts[f.nextID] = body
		f.writeJSON(w, 201, map[string]any{"data": present(f.nextID, body, defaults(sourceID))})
	case seg != "":
		id, _ := strconv.ParseInt(seg, 10, 64)
		a, ok := f.alerts[id]
		if !ok || a["source_id"] != any(sourceID) {
			f.writeJSON(w, 404, nil)
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.writeJSON(w, 200, map[string]any{"data": present(id, a, defaults(sourceID))})
		case http.MethodPut:
			body["source_id"] = sourceID
			f.alerts[id] = body
			f.writeJSON(w, 200, map[string]any{"data": present(id, body, defaults(sourceID))})
		case http.MethodDelete:
			delete(f.alerts, id)
			f.writeJSON(w, 204, nil)
		}
	default:
		f.writeJSON(w, 404, nil)
	}
}
