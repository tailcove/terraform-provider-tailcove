package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Request side: a null attribute is sent as JSON null, so a PUT (which replaces the whole form) clears it.

func optStr(v types.String) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueString()
}

func optInt(v types.Int64) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueInt64()
}

func optFloat(v types.Float64) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueFloat64()
}

func optBool(v types.Bool) any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBool()
}

// Response side. The API does not return a secret and fills in defaults for fields the configuration
// never mentioned, so a refresh only updates an attribute the configuration (or the state) already
// holds. `force` is true while importing, when nothing is held yet and everything the API knows is taken.

func refStr(cur types.String, api *string, force bool) types.String {
	if !force && cur.IsNull() {
		return cur
	}
	if api == nil {
		return types.StringNull()
	}
	return types.StringValue(*api)
}

func refInt(cur types.Int64, api *int64, force bool) types.Int64 {
	if !force && cur.IsNull() {
		return cur
	}
	if api == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*api)
}

func refFloat(cur types.Float64, api *float64, force bool) types.Float64 {
	if !force && cur.IsNull() {
		return cur
	}
	if api == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*api)
}

// strOrNull is for a computed attribute: always taken from the API.
func strOrNull(api *string) types.String {
	if api == nil {
		return types.StringNull()
	}
	return types.StringValue(*api)
}

func intOrNull(api *int64) types.Int64 {
	if api == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*api)
}

// idRe is the shape of a Tailcove id: a short prefix for what the thing is and a lowercase ULID,
// for example src_01jbx5q8k7m3n2p4r6s8t0v1w3. Ids are opaque: the provider never reads anything into them.
var idRe = regexp.MustCompile(`^[a-z]{1,8}_[0-9a-hjkmnp-tv-z]{26}$`)

func parseID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !idRe.MatchString(s) {
		return "", fmt.Errorf("%q is not a Tailcove id (expected a prefix and a ULID, such as src_01jbx5q8k7m3n2p4r6s8t0v1w3)", s)
	}
	return s, nil
}

func addAPIError(d *diag.Diagnostics, summary string, err error) {
	d.AddError(summary, err.Error())
}
