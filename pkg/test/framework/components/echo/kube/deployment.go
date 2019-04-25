// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kube

import (
	"bufio"
	"bytes"
	"fmt"
	"text/template"

	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/echo"
)

const (
	deploymentYAML = `
{{- if .ServiceAccount }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Service }}
  labels:
    app: {{ .Service }}
spec:
{{- if .Headless }}
  clusterIP: None
{{- end }}
  ports:
{{- range $i, $p := .Ports }}
  - name: {{ $p.Name }}
    port: {{ $p.ServicePort }}
    targetPort: {{ $p.InstancePort }}
{{- end }}
  selector:
    app: {{ .Service }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Service }}-{{ .Version }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ .Service }}
      version: {{ .Version }}
{{- if ne .Locality "" }}
      istio-locality: {{ .Locality }}
{{- end }}
  template:
    metadata:
      labels:
        app: {{ .Service }}
        version: {{ .Version }}
{{- if ne .Locality "" }}
        istio-locality: {{ .Locality }}
{{- end }}
{{- if not .Sidecar }}
      annotations:
        sidecar.istio.io/inject: "false"
{{- end }}
    spec:
{{- if .ServiceAccount }}
      serviceAccountName: {{ .Service }}
{{- end }}
      containers:
      - name: app
        image: {{ .Hub }}/app:{{ .Tag }}
        imagePullPolicy: {{ .PullPolicy }}
        args:
{{- range $i, $p := .ContainerPorts }}
{{- if eq .Protocol "GRPC" }}
          - --grpc
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- end }}
          - --version
          - "{{ .Version }}"
        ports:
{{- range $i, $p := .ContainerPorts }}
        - containerPort: {{ $p.Port }} 
{{- if eq .Port 3333 }}
          name: tcp-health-port
{{- end }}
{{- end }}
        readinessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
        livenessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
---
apiVersion: v1
kind: Secret
metadata:
  name: sdstokensecret
type: Opaque
stringData:
  sdstoken: "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2\
VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Ii\
wia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InZhdWx0LWNpdGFkZWwtc2\
EtdG9rZW4tcmZxZGoiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC\
5uYW1lIjoidmF1bHQtY2l0YWRlbC1zYSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2Vydm\
ljZS1hY2NvdW50LnVpZCI6IjIzOTk5YzY1LTA4ZjMtMTFlOS1hYzAzLTQyMDEwYThhMDA3OSIsInN1Yi\
I6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnZhdWx0LWNpdGFkZWwtc2EifQ.RNH1QbapJKP\
mktV3tCnpiz7hoYpv1TM6LXzThOtaDp7LFpeANZcJ1zVQdys3EdnlkrykGMepEjsdNuT6ndHfh8jRJAZ\
uNWNPGrhxz4BeUaOqZg3v7AzJlMeFKjY_fiTYYd2gBZZxkpv1FvAPihHYng2NeN2nKbiZbsnZNU1qFdv\
bgCISaFqTf0dh75OzgCX_1Fh6HOA7ANf7p522PDW_BRln0RTwUJovCpGeiNCGdujGiNLDZyBcdtikY5r\
y_KXTdrVAcTUvI6lxwRbONNfuN8hrIDl95vJjhUlE-O-_cx8qWtXNdqJlMje1SsiPCL4uq70OepG_I4a\
SzC2o8aDtlQ"
---
`
)

var (
	deploymentTemplate *template.Template
)

func init() {
	deploymentTemplate = template.New("echo_deployment")
	if _, err := deploymentTemplate.Parse(deploymentYAML); err != nil {
		panic(fmt.Sprintf("unable to parse echo deployment template: %v", err))
	}
}

func generateYAML(cfg echo.Config) (string, error) {
	// Create the parameters for the YAML template.
	settings, err := deployment.SettingsFromCommandLine()
	if err != nil {
		return "", err
	}

	params := map[string]interface{}{
		"Hub":            settings.Hub,
		"Tag":            settings.Tag,
		"PullPolicy":     settings.PullPolicy,
		"Service":        cfg.Service,
		"Version":        cfg.Version,
		"Sidecar":        cfg.Sidecar,
		"Headless":       cfg.Headless,
		"Locality":       cfg.Locality,
		"ServiceAccount": cfg.ServiceAccount,
		"Ports":          cfg.Ports,
		"ContainerPorts": getContainerPorts(cfg.Ports),
	}

	// Generate the YAML content.
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := deploymentTemplate.Execute(w, params); err != nil {
		return "", err
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	return filled.String(), nil
}