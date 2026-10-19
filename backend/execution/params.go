package execution

import (
	"encoding/json"
	"math"
	"slices"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

func integral(v any) bool {
	switch n := v.(type) {
	case int, int32, int64:
		return true
	case float64:
		return n == math.Trunc(n) && !math.IsInf(n, 0)
	case json.Number:
		_, err := n.Int64()
		return err == nil
	}
	return false
}

func numeric(v any) bool {
	switch n := v.(type) {
	case int, int32, int64, float32:
		return true
	case float64:
		return !math.IsNaN(n) && !math.IsInf(n, 0)
	case json.Number:
		_, err := n.Float64()
		return err == nil
	}
	return false
}

func paramOK(def ir.ParamDef, v any) bool {
	switch def.Type {
	case ir.ValueTypeText, ir.ValueTypeReference, ir.ValueTypeDate, ir.ValueTypeBinary:
		_, ok := v.(string)
		return ok
	case ir.ValueTypeInteger, ir.ValueTypeInstant:
		return integral(v)
	case ir.ValueTypeDecimal:
		return numeric(v)
	case ir.ValueTypeBoolean:
		_, ok := v.(bool)
		return ok
	case ir.ValueTypeChoice:
		s, ok := v.(string)
		if !ok {
			return false
		}
		return def.Choices != nil && slices.Contains(labelStrings(*def.Choices), s)
	}
	return false
}

func labelStrings(in []ir.Label) []string {
	out := make([]string, len(in))
	for i, l := range in {
		out[i] = string(l)
	}
	return out
}

// checkParams accepts exactly the inputs the contract declares, with the declared types.
func checkParams(inputs map[ir.ParamName]ir.ParamDef, params map[string]any) error {
	for name := range params {
		if _, ok := inputs[ir.ParamName(name)]; !ok {
			return refuse("param_unknown:" + name)
		}
	}
	for name, def := range inputs {
		v, ok := params[string(name)]
		if !ok || v == nil {
			if def.Required {
				return refuse("param_missing:" + string(name))
			}
			continue
		}
		if !paramOK(def, v) {
			return refuse("param_invalid:" + string(name))
		}
	}
	return nil
}
