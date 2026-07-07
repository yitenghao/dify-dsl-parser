package engine

import (
	"encoding/json"
	"fmt"

	"dify-dsl-parser/dsl"
)

// RenderTemplate replaces every {{#node.var.path#}} reference in template
// with the corresponding value pulled from pool.
//
// When a reference can't be resolved, the placeholder is left as-is so the
// failure is visible in the output (the same behaviour as graphon's
// VariableTemplateParser when convert_template returns missing segments).
//
// Non-string values are stringified:
//   - bool / int / float / nil : Go fmt default
//   - map / slice              : json.Marshal (without HTML escaping)
//   - string                   : returned verbatim
func RenderTemplate(template string, pool *VariablePool) string {
	refs := dsl.ExtractTemplateRefs(template)
	if len(refs) == 0 {
		return template
	}
	out := template
	for _, ref := range refs {
		v, ok := pool.Get(ref.Selector)
		if !ok {
			// Leave the original {{#...#}} placeholder so debugging is easy.
			continue
		}
		// Replace ALL occurrences of the same raw expression.
		out = replaceAll(out, ref.Raw, stringify(v))
	}
	return out
}

// replaceAll is a tiny strings.Replace that avoids importing strings just
// for this hot path; written inline to keep the engine package's import
// surface lean.
func replaceAll(s, old, new string) string {
	if old == "" || old == new {
		return s
	}
	// Use strings.ReplaceAll equivalent without importing strings here.
	// This is fine for our short templates; for very long inputs callers
	// should batch their substitutions.
	var buf []byte
	for {
		idx := index(s, old)
		if idx < 0 {
			buf = append(buf, s...)
			break
		}
		buf = append(buf, s[:idx]...)
		buf = append(buf, new...)
		s = s[idx+len(old):]
	}
	return string(buf)
}

func index(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// stringify converts an arbitrary variable-pool value into the textual form
// used inside rendered templates. The rules match what graphon's
// SegmentGroup.markdown produces in the common cases.
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int, int32, int64, uint, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32:
		return fmt.Sprintf("%g", x)
	case float64:
		return fmt.Sprintf("%g", x)
	default:
		// Fall through to JSON for maps / slices / structs / etc.
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}
