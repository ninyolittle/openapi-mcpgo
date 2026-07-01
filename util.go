package openapi_mcpgo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func coalesce[T comparable](vals ...T) T {
	var zero T
	for _, val := range vals {
		if val != zero {
			return val
		}
	}
	return zero
}

var pathParamRE = regexp.MustCompile(`\{[^}]+\}`)

func resolvePath(
	path string,
	params []*openapi2.Parameter,
	args map[string]any) string {
	vals := make(map[string]any, 0)
	for _, param := range params {
		if param.In != "path" {
			continue
		}

		vals[param.Name] = args[param.Name]
	}

	result := pathParamRE.ReplaceAllStringFunc(path, func(m string) string {
		name := m[1 : len(m)-1]
		if val, ok := vals[name]; ok {
			return val.(string)
		}
		return m
	})
	return result
}

func resolveQuery(
	params []*openapi2.Parameter,
	args map[string]any,
) string {
	qv := url.Values{}

	for _, param := range params {
		if param.In != "query" {
			continue
		}

		if val, ok := args[param.Name]; ok {
			qv.Set(param.Name, val.(string))
		}
	}

	return qv.Encode()
}

func resolveBody(params []*openapi2.Parameter, args map[string]any) (io.Reader, error) {
	m := make(map[string]any, 0)
	for _, param := range params {
		if param.In != "body" {
			continue
		}

		values, ok := args["body"].(map[string]any)
		if !ok {
			continue
		}

		maps.Copy(m, values)
	}

	if len(m) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	log.Printf("resolveBody: %v", m)

	return bytes.NewReader(payload), nil
}

// buildFullURL constructs the full URL for an API request based on the OpenAPI specification, path parameters, and query parameters.
func (o *OpenApiTools) buildFullURL(path string, op *openapi2.Operation, method string, args map[string]any) string {
	p := resolvePath(path, op.Parameters, args)
	q := resolveQuery(op.Parameters, args)

	fullURL := o.scheme + "://" + o.doc.Host + o.doc.BasePath + p
	if q != "" {
		fullURL += "?" + q
	}

	if o.urlBuilder != nil {
		fullURL = o.urlBuilder(op, method, o.scheme, o.doc.Host, o.doc.BasePath, p, q)
	}

	return fullURL
}

func (o *OpenApiTools) buildHandler(
	path string,
	operation *openapi2.Operation,
	method string,
	ctx context.Context,
	req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	log.Println(args)

	fullURL := o.buildFullURL(path, operation, method, args)

	payload, err := resolveBody(operation.Parameters, args)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, payload)
	if err != nil {
		return nil, err
	}
	headers, err := o.headers(ctx)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(string(body)), nil
}

// preprocessToolOpts processes the parameters of an OpenAPI operation and
// generates corresponding MCP tool options based on their types and properties.
func preprocessToolOpts(
	doc *openapi2.T,
	toolOpts *[]mcp.ToolOption, operation *openapi2.Operation) {
	for _, param := range operation.Parameters {
		propOpts := []mcp.PropertyOption{
			mcp.Description(param.Description),
		}

		if param.Required {
			propOpts = append(propOpts, mcp.Required())
		}

		switch {
		case param.Type.Is("string"):
			*toolOpts = append(*toolOpts, mcp.WithString(param.Name, propOpts...))
		case param.Type.Is("number"):
			*toolOpts = append(*toolOpts, mcp.WithNumber(param.Name, propOpts...))
		case param.Type.Is("integer"):
			*toolOpts = append(*toolOpts, mcp.WithInteger(param.Name, propOpts...))
		case param.Type.Is("boolean"):
			*toolOpts = append(*toolOpts, mcp.WithBoolean(param.Name, propOpts...))
		case param.Type.Is("array"):
			*toolOpts = append(*toolOpts, mcp.WithArray(param.Name, propOpts...))
		default:
			propOpts = append(propOpts, mcp.Properties(schemaToJsonSchema(doc, getSchemaFromRef(doc, param.Schema.Ref))["properties"].(map[string]any)))
			*toolOpts = append(*toolOpts, mcp.WithObject(param.Name, propOpts...))
		}
	}
	// log.Println(len(*toolOpts))
}

func schemaToJsonSchema(doc *openapi2.T, schema *openapi2.Schema) map[string]any {
	m := make(map[string]any, 0)

	if !schema.Type.IsEmpty() {
		m["type"] = schema.Type
	}

	if schema.Description != "" {
		m["description"] = schema.Description
	}

	if len(schema.Required) > 0 {
		m["required"] = schema.Required
	}

	if items := schema.Items; items != nil {
		sc := schemaToJsonSchema(doc, items.Value)
		if items.Ref != "" {
			sc = schemaToJsonSchema(doc, getSchemaFromRef(doc, items.Ref))
		}
		m["items"] = sc
	}

	if schema.Enum != nil {
		m["enum"] = schema.Enum
	}

	if schema.Format != "" {
		m["format"] = schema.Format
	}

	if schema.Pattern != "" {
		m["pattern"] = schema.Pattern
	}

	if schema.Default != nil {
		m["default"] = schema.Default
	}

	if len(schema.Properties) > 0 {
		props := make(map[string]any)
		for propName, propSchema := range schema.Properties {
			if val := propSchema.Value; val != nil && val.Items != nil && val.Items.Ref != "" {
				props[propName] = schemaToJsonSchema(doc, getSchemaFromRef(doc, val.Items.Ref))
			} else if propSchema.Ref != "" {
				props[propName] = schemaToJsonSchema(doc, getSchemaFromRef(doc, propSchema.Ref))
			} else {
				props[propName] = schemaToJsonSchema(doc, propSchema.Value)
			}

		}
		m["properties"] = props
	}
	return m
}

func getSchemaFromRef(doc *openapi2.T, ref string) *openapi2.Schema {
	s := strings.TrimPrefix(ref, "#/definitions/")
	r, ok := doc.Definitions[s]
	if !ok {
		log.Printf("Definition not found for ref: %s", ref)
		return nil
	}
	return r.Value
}

func (o *OpenApiTools) buildTools() ([]server.ServerTool, error) {
	tools := make([]server.ServerTool, 0)

	for path, pathItem := range o.doc.Paths {
		for method, operation := range pathItem.Operations() {
			if operation == nil {
				continue
			}

			readOnly := method == "GET"
			destructive := method == "DELETE"

			toolOpts := []mcp.ToolOption{
				mcp.WithDescription(coalesce(operation.Summary, operation.Description)),
				mcp.WithReadOnlyHintAnnotation(readOnly),
				mcp.WithDestructiveHintAnnotation(destructive),
			}

			preprocessToolOpts(o.doc, &toolOpts, operation)

			toolName := operation.OperationID
			if toolName == "" {
				toolName = fmt.Sprintf("%s_%s", method, path)
			}

			tools = append(tools, server.ServerTool{
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return o.buildHandler(
						path, operation, method, ctx, request)
				},
				Tool: mcp.NewTool(toolName, toolOpts...),
			})

		}
	}

	return tools, nil
}
