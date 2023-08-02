package cri

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	internalapi "k8s.io/cri-api/pkg/apis"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/kubernetes/pkg/kubelet/cri/remote"
)

const (
	defaultTimeout  = 2 * time.Second
	RuntimeEndpoint = "unix:///var/run/containerd/containerd.sock"
	ImageEndpoint   = "unix:///var/run/containerd/containerd.sock"
)

type CRI interface {
	ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error)
	GetContainerPiD(ctx context.Context, containerID string) (any, error)
}

type cri struct {
	timeout       time.Duration
	runtimeClient internalapi.RuntimeService
	imageClient   internalapi.ImageManagerService
}

func New() (CRI, error) {
	runtimeClient, err := remote.NewRemoteRuntimeService(RuntimeEndpoint, defaultTimeout, nil)
	if err != nil {
		return nil, err
	}
	imageClient, err := remote.NewRemoteImageService(ImageEndpoint, defaultTimeout, nil)
	if err != nil {
		return nil, err
	}

	return &cri{
		timeout:       defaultTimeout,
		runtimeClient: runtimeClient,
		imageClient:   imageClient,
	}, nil
}

func (r *cri) ListContainers(ctx context.Context, filter *criv1.ContainerFilter) ([]*criv1.Container, error) {
	return r.runtimeClient.ListContainers(ctx, filter)
}

func (r *cri) GetContainerPiD(ctx context.Context, containerID string) (any, error) {
	resp, err := r.runtimeClient.ContainerStatus(ctx, containerID, true)
	if err != nil {
		return "", err
	}

	status, err := marshalContainerStatus(resp.Status)
	if err != nil {
		return "", err
	}

	return outputStatusInfo(status, resp.Info, "{{.info.pid}}")

	/*
		x := map[string]any{}
		if err := json.Unmarshal([]byte(resp.GetInfo()["info"]), &x); err != nil {
			return "", err
		}
		//log.Infof("leaf1: pid: %v", x["pid"])
		//log.Infof("leaf1: pid type: %v", reflect.TypeOf(x["pid"]).Name())

		return x["pid"], nil
	*/
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

func builtinTmplFuncs() template.FuncMap {
	return template.FuncMap{
		"json":  jsonBuiltinTmplFunc,
		"title": strings.Title,
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

// marshalContainerStatus converts container status into string and converts
// the timestamps into readable format.
func marshalContainerStatus(cs *criv1.ContainerStatus) (string, error) {
	statusStr, err := protobufObjectToJSON(cs)
	if err != nil {
		return "", err
	}
	jsonMap := make(map[string]interface{})
	err = json.Unmarshal([]byte(statusStr), &jsonMap)
	if err != nil {
		return "", err
	}

	jsonMap["createdAt"] = time.Unix(0, cs.CreatedAt).Format(time.RFC3339Nano)
	var startedAt, finishedAt time.Time
	if cs.State != criv1.ContainerState_CONTAINER_CREATED {
		// If container is not in the created state, we have tried and
		// started the container. Set the startedAt.
		startedAt = time.Unix(0, cs.StartedAt)
	}
	if cs.State == criv1.ContainerState_CONTAINER_EXITED ||
		(cs.State == criv1.ContainerState_CONTAINER_UNKNOWN && cs.FinishedAt > 0) {
		// If container is in the exit state, set the finishedAt.
		// Or if container is in the unknown state and FinishedAt > 0, set the finishedAt
		finishedAt = time.Unix(0, cs.FinishedAt)
	}
	jsonMap["startedAt"] = startedAt.Format(time.RFC3339Nano)
	jsonMap["finishedAt"] = finishedAt.Format(time.RFC3339Nano)
	return marshalMapInOrder(jsonMap, *cs)
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

func protobufObjectToJSON(obj proto.Message) (string, error) {
	jsonpbMarshaler := jsonpb.Marshaler{EmitDefaults: true, Indent: "  "}
	marshaledJSON, err := jsonpbMarshaler.MarshalToString(obj)
	if err != nil {
		return "", err
	}
	return marshaledJSON, nil
}
