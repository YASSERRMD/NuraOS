package configmgr

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// DriftEntry describes one field that differs between the snapshot and
// the running configuration.
type DriftEntry struct {
	// Field is the JSON path (e.g. "gateway.port").
	Field string `json:"field"`
	// Snapshot is the value in the stored snapshot.
	Snapshot interface{} `json:"snapshot"`
	// Running is the current live value.
	Running interface{} `json:"running"`
}

// DriftReport is the result of comparing a snapshot to a running config.
type DriftReport struct {
	// Drifted is true when at least one field differs.
	Drifted bool `json:"drifted"`
	// Entries lists every differing field.
	Entries []DriftEntry `json:"entries,omitempty"`
}

// DetectDrift compares snapshot to running and returns a DriftReport.
// It works by flattening both configs to a JSON map and comparing leaves.
func DetectDrift(snapshot, running Config) DriftReport {
	snapMap := flatten("", snapshot)
	runMap := flatten("", running)

	var entries []DriftEntry
	for k, sv := range snapMap {
		rv, ok := runMap[k]
		if !ok || !reflect.DeepEqual(sv, rv) {
			entries = append(entries, DriftEntry{Field: k, Snapshot: sv, Running: rv})
		}
	}
	// Also catch keys present in running but absent in snapshot.
	for k, rv := range runMap {
		if _, ok := snapMap[k]; !ok {
			entries = append(entries, DriftEntry{Field: k, Snapshot: nil, Running: rv})
		}
	}
	return DriftReport{Drifted: len(entries) > 0, Entries: entries}
}

// flatten marshals v to JSON then walks the decoded map to produce a flat
// map with dotted key paths (e.g. {"gateway.port": 8080}).
func flatten(prefix string, v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	var m interface{}
	_ = json.Unmarshal(data, &m)
	out := make(map[string]interface{})
	flattenInto(out, prefix, m)
	return out
}

func flattenInto(out map[string]interface{}, prefix string, v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flattenInto(out, key, child)
		}
	case []interface{}:
		for i, child := range val {
			key := fmt.Sprintf("%s[%d]", prefix, i)
			flattenInto(out, key, child)
		}
	default:
		out[prefix] = v
	}
}
