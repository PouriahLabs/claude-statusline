package config

import (
	"reflect"
	"strings"
)

// Minimal reduces c to the keys worth writing to disk: those that differ from
// the built-in defaults, plus any keep paths ("display.icons") forced in even
// when they match.
//
// The wizard used to encode the whole struct, which pinned all ~50 defaults at
// install time. Load layers a user's file over the defaults, so a pinned key
// wins forever -- a later improvement to a colour, a threshold or a model name
// could never reach anyone who had run the wizard, and the only fix was to tell
// people to delete their config. Writing just the deltas keeps the rest live.
//
// The keep paths are the exception, and they are the questions the wizard
// actually asked. An icon tier is a claim about the user's font that no future
// default can know better, so an explicit answer is pinned even when it happens
// to match today's default.
func Minimal(c Config, keep ...string) map[string]any {
	cur, def := reflect.ValueOf(c), reflect.ValueOf(Default())
	out := diffStruct(cur, def)
	for _, path := range keep {
		if v, ok := lookup(cur, path); ok {
			setPath(out, path, v)
		}
	}
	return out
}

// tomlName is the key a field is written under, or "" if it is not written.
func tomlName(f reflect.StructField) string {
	tag := f.Tag.Get("toml")
	if tag == "" || tag == "-" {
		return ""
	}
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

// diffStruct walks two values of the same struct type and collects the fields
// that differ. Nested structs recurse so an untouched table is omitted whole
// rather than written out as a header with nothing under it.
func diffStruct(cur, def reflect.Value) map[string]any {
	out := map[string]any{}
	t := cur.Type()
	for i := 0; i < t.NumField(); i++ {
		name := tomlName(t.Field(i))
		if name == "" {
			continue
		}
		cv, dv := cur.Field(i), def.Field(i)
		if cv.Kind() == reflect.Struct {
			if sub := diffStruct(cv, dv); len(sub) > 0 {
				out[name] = sub
			}
			continue
		}
		// Slices and maps (order, context_window, model_names) are compared and
		// written whole: a partial list would not mean "the rest as shipped", it
		// would mean the rest is gone.
		if !reflect.DeepEqual(cv.Interface(), dv.Interface()) {
			out[name] = cv.Interface()
		}
	}
	return out
}

func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		sub, ok := m[p].(map[string]any)
		if !ok {
			sub = map[string]any{}
			m[p] = sub
		}
		m = sub
	}
	m[parts[len(parts)-1]] = v
}

func lookup(v reflect.Value, path string) (any, bool) {
	for _, p := range strings.Split(path, ".") {
		if v.Kind() != reflect.Struct {
			return nil, false
		}
		t, found := v.Type(), false
		for i := 0; i < t.NumField(); i++ {
			if tomlName(t.Field(i)) == p {
				v, found = v.Field(i), true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return v.Interface(), true
}
