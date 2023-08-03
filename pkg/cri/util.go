package cri

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

func protobufObjectToJSON(obj proto.Message) (string, error) {
	jsonpbMarshaler := jsonpb.Marshaler{EmitDefaults: true, Indent: "  "}
	marshaledJSON, err := jsonpbMarshaler.MarshalToString(obj)
	if err != nil {
		return "", err
	}
	return marshaledJSON, nil
}

func outputStatusInfo(status string, info map[string]string, tmplStr string) (string, error) {
	// Sort all keys
	keys := []string{}
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	jsonInfo := "{" + "\"status\":" + status + ","
	for _, k := range keys {
		var res interface{}
		// We attempt to convert key into JSON if possible else use it directly
		if err := json.Unmarshal([]byte(info[k]), &res); err != nil {
			jsonInfo += "\"" + k + "\"" + ":" + "\"" + info[k] + "\","
		} else {
			jsonInfo += "\"" + k + "\"" + ":" + info[k] + ","
		}
	}
	jsonInfo = jsonInfo[:len(jsonInfo)-1]
	jsonInfo += "}"

	return tmplExecuteRawJSON(tmplStr, jsonInfo)
}

// marshalMapInOrder marshalls a map into json in the order of the original
// data structure.
func marshalMapInOrder(m map[string]interface{}, t interface{}) (string, error) {
	s := "{"
	v := reflect.ValueOf(t)
	for i := 0; i < v.Type().NumField(); i++ {
		field := jsonFieldFromTag(v.Type().Field(i).Tag)
		if field == "" || field == "-" {
			continue
		}
		value, err := json.Marshal(m[field])
		if err != nil {
			return "", err
		}
		s += fmt.Sprintf("%q:%s,", field, value)
	}
	s = s[:len(s)-1]
	s += "}"
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// jsonFieldFromTag gets json field name from field tag.
func jsonFieldFromTag(tag reflect.StructTag) string {
	field := strings.Split(tag.Get("json"), ",")[0]
	for _, f := range strings.Split(tag.Get("protobuf"), ",") {
		if !strings.HasPrefix(f, "json=") {
			continue
		}
		field = strings.TrimPrefix(f, "json=")
	}
	return field
}
