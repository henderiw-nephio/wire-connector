package cri

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
)

func builtinTmplFuncs() template.FuncMap {
	return template.FuncMap{
		"json": jsonBuiltinTmplFunc,
		//"title": strings.Title,
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
	}
}

// jsonBuiltinTmplFunc allows to jsonify result of template execution.
func jsonBuiltinTmplFunc(v interface{}) string {
	o := new(bytes.Buffer)
	enc := json.NewEncoder(o)
	// FIXME(fuweid): should we panic?
	enc.Encode(v)
	return o.String()
}

// tmplExecuteRawJSON executes the template with interface{} with decoded by
// rawJSON string.
func tmplExecuteRawJSON(tmplStr string, rawJSON string) (string, error) {
	dec := json.NewDecoder(
		bytes.NewReader([]byte(rawJSON)),
	)
	dec.UseNumber()

	var raw interface{}
	if err := dec.Decode(&raw); err != nil {
		return "", fmt.Errorf("failed to decode json: %w", err)
	}

	var o = new(bytes.Buffer)
	tmpl, err := template.New("tmplExecuteRawJSON").Funcs(builtinTmplFuncs()).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to generate go-template: %w", err)
	}

	// return error if key doesn't exist
	tmpl = tmpl.Option("missingkey=error")
	if err := tmpl.Execute(o, raw); err != nil {
		return "", fmt.Errorf("failed to template data: %w", err)
	}
	return o.String(), nil
}
