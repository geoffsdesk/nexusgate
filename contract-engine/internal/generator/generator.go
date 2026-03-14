package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nexusgate/contract-engine/internal/store"
)

// Generator produces typed SDK code and API specs from contracts.
type Generator struct{}

func New() *Generator {
	return &Generator{}
}

// GenerateTypeScript produces a TypeScript client SDK from a contract.
func (g *Generator) GenerateTypeScript(contract *store.Contract) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("// NexusGate SDK — %s v%s\n", contract.Name, contract.Version))
	sb.WriteString("// Auto-generated. Do not edit manually.\n\n")

	// Generate types
	for _, t := range contract.Types {
		sb.WriteString(fmt.Sprintf("export interface %s {\n", t.Name))
		for field, typ := range t.Fields {
			tsType := mapToTSType(typ)
			sb.WriteString(fmt.Sprintf("  %s: %s;\n", field, tsType))
		}
		sb.WriteString("}\n\n")
	}

	// Generate client class
	sb.WriteString(fmt.Sprintf("export class %sClient {\n", toPascalCase(contract.Name)))
	sb.WriteString("  private baseURL: string;\n")
	sb.WriteString("  private headers: Record<string, string>;\n\n")
	sb.WriteString("  constructor(baseURL: string, apiKey?: string) {\n")
	sb.WriteString("    this.baseURL = baseURL.replace(/\\/$/, '');\n")
	sb.WriteString("    this.headers = {\n")
	sb.WriteString("      'Content-Type': 'application/json',\n")
	sb.WriteString("      ...(apiKey ? { 'Authorization': `Bearer ${apiKey}` } : {}),\n")
	sb.WriteString("    };\n")
	sb.WriteString("  }\n\n")

	// Generate methods
	for _, ep := range contract.Endpoints {
		sb.WriteString(g.generateTSMethod(ep))
	}

	// Private fetch helper
	sb.WriteString("  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {\n")
	sb.WriteString("    const resp = await fetch(`${this.baseURL}${path}`, {\n")
	sb.WriteString("      method,\n")
	sb.WriteString("      headers: this.headers,\n")
	sb.WriteString("      body: body ? JSON.stringify(body) : undefined,\n")
	sb.WriteString("    });\n")
	sb.WriteString("    if (!resp.ok) throw new Error(`API error: ${resp.status} ${resp.statusText}`);\n")
	sb.WriteString("    return resp.json();\n")
	sb.WriteString("  }\n")
	sb.WriteString("}\n")

	return sb.String()
}

func (g *Generator) generateTSMethod(ep store.Endpoint) string {
	var sb strings.Builder

	methodName := toCamelCase(ep.Operation)
	returnType := ep.Output

	// Build parameters
	var params []string
	for name, typ := range ep.Input {
		params = append(params, fmt.Sprintf("%s: %s", name, mapToTSType(typ)))
	}

	sb.WriteString(fmt.Sprintf("  /** %s */\n", ep.Description))
	sb.WriteString(fmt.Sprintf("  async %s(%s): Promise<%s> {\n", methodName, strings.Join(params, ", "), returnType))

	// Build path with parameter substitution
	path := ep.Path
	for name := range ep.Input {
		path = strings.ReplaceAll(path, fmt.Sprintf("{%s}", name), fmt.Sprintf("${%s}", name))
	}

	if ep.Method == "GET" || ep.Method == "DELETE" {
		sb.WriteString(fmt.Sprintf("    return this.request<%s>('%s', `%s`);\n", returnType, ep.Method, path))
	} else {
		sb.WriteString(fmt.Sprintf("    return this.request<%s>('%s', `%s`, { %s });\n",
			returnType, ep.Method, path, strings.Join(getParamNames(ep.Input), ", ")))
	}

	sb.WriteString("  }\n\n")
	return sb.String()
}

// GeneratePython produces a Python client from a contract.
func (g *Generator) GeneratePython(contract *store.Contract) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\"\"\"NexusGate SDK — %s v%s\nAuto-generated. Do not edit manually.\"\"\"\n\n", contract.Name, contract.Version))
	sb.WriteString("from dataclasses import dataclass\nfrom typing import Optional, List\nimport httpx\n\n")

	// Generate dataclasses
	for _, t := range contract.Types {
		sb.WriteString("@dataclass\n")
		sb.WriteString(fmt.Sprintf("class %s:\n", t.Name))
		for field, typ := range t.Fields {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", toSnakeCase(field), mapToPyType(typ)))
		}
		sb.WriteString("\n")
	}

	// Generate client class
	sb.WriteString(fmt.Sprintf("class %sClient:\n", toPascalCase(contract.Name)))
	sb.WriteString("    def __init__(self, base_url: str, api_key: Optional[str] = None):\n")
	sb.WriteString("        self.base_url = base_url.rstrip('/')\n")
	sb.WriteString("        self.headers = {'Content-Type': 'application/json'}\n")
	sb.WriteString("        if api_key:\n")
	sb.WriteString("            self.headers['Authorization'] = f'Bearer {api_key}'\n")
	sb.WriteString("        self._client = httpx.Client(base_url=self.base_url, headers=self.headers)\n\n")

	for _, ep := range contract.Endpoints {
		methodName := toSnakeCase(ep.Operation)
		var params []string
		for name, typ := range ep.Input {
			params = append(params, fmt.Sprintf("%s: %s", toSnakeCase(name), mapToPyType(typ)))
		}

		sb.WriteString(fmt.Sprintf("    def %s(self, %s) -> %s:\n", methodName, strings.Join(params, ", "), ep.Output))
		sb.WriteString(fmt.Sprintf("        \"\"\"%s\"\"\"\n", ep.Description))

		path := ep.Path
		for name := range ep.Input {
			path = strings.ReplaceAll(path, fmt.Sprintf("{%s}", name), fmt.Sprintf("{%s}", toSnakeCase(name)))
		}

		sb.WriteString(fmt.Sprintf("        resp = self._client.request('%s', f'%s')\n", ep.Method, path))
		sb.WriteString("        resp.raise_for_status()\n")
		sb.WriteString("        return resp.json()\n\n")
	}

	return sb.String()
}

// GenerateOpenAPI produces an OpenAPI 3.0 spec from a contract.
func (g *Generator) GenerateOpenAPI(contract *store.Contract) map[string]interface{} {
	paths := map[string]interface{}{}

	for _, ep := range contract.Endpoints {
		method := strings.ToLower(ep.Method)
		operation := map[string]interface{}{
			"operationId": ep.Operation,
			"summary":     ep.Description,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Successful response",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"$ref": fmt.Sprintf("#/components/schemas/%s", ep.Output),
							},
						},
					},
				},
			},
		}

		if _, ok := paths[ep.Path]; !ok {
			paths[ep.Path] = map[string]interface{}{}
		}
		paths[ep.Path].(map[string]interface{})[method] = operation
	}

	schemas := map[string]interface{}{}
	for _, t := range contract.Types {
		properties := map[string]interface{}{}
		for field, typ := range t.Fields {
			properties[field] = map[string]interface{}{"type": mapToOpenAPIType(typ)}
		}
		schemas[t.Name] = map[string]interface{}{
			"type":       "object",
			"properties": properties,
		}
	}

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       contract.Name,
			"version":     contract.Version,
			"description": contract.Description,
		},
		"paths": paths,
		"components": map[string]interface{}{
			"schemas": schemas,
		},
	}
}

// ── HTTP Handlers ──

func (g *Generator) HandleGenerateTS(w http.ResponseWriter, r *http.Request) {
	var contract store.Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, `{"error":"invalid contract"}`, http.StatusBadRequest)
		return
	}
	code := g.GenerateTypeScript(&contract)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(code))
}

func (g *Generator) HandleGeneratePython(w http.ResponseWriter, r *http.Request) {
	var contract store.Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, `{"error":"invalid contract"}`, http.StatusBadRequest)
		return
	}
	code := g.GeneratePython(&contract)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(code))
}

func (g *Generator) HandleGenerateGo(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement Go SDK generation
	http.Error(w, `{"error":"Go generation not yet implemented"}`, http.StatusNotImplemented)
}

func (g *Generator) HandleGenerateOpenAPI(w http.ResponseWriter, r *http.Request) {
	var contract store.Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, `{"error":"invalid contract"}`, http.StatusBadRequest)
		return
	}
	spec := g.GenerateOpenAPI(&contract)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

// ── Helpers ──

func mapToTSType(t string) string {
	switch t {
	case "string", "uuid", "email", "datetime":
		return "string"
	case "integer", "int", "int64", "float", "number":
		return "number"
	case "boolean", "bool":
		return "boolean"
	default:
		return t
	}
}

func mapToPyType(t string) string {
	switch t {
	case "string", "uuid", "email", "datetime":
		return "str"
	case "integer", "int", "int64":
		return "int"
	case "float", "number":
		return "float"
	case "boolean", "bool":
		return "bool"
	default:
		return t
	}
}

func mapToOpenAPIType(t string) string {
	switch t {
	case "string", "uuid", "email", "datetime":
		return "string"
	case "int", "int64", "integer":
		return "integer"
	case "float", "number":
		return "number"
	case "boolean", "bool":
		return "boolean"
	default:
		return "string"
	}
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toCamelCase(s string) string {
	p := toPascalCase(s)
	if len(p) > 0 {
		return strings.ToLower(p[:1]) + p[1:]
	}
	return p
}

func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, r+32)
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

func getParamNames(input map[string]string) []string {
	names := make([]string, 0, len(input))
	for name := range input {
		names = append(names, name)
	}
	return names
}
