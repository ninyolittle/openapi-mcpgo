package openapi_mcpgo

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/golang/glog"
	"github.com/mark3labs/mcp-go/server"
)

type OpenApiTools struct {
	doc *openapi2.T

	urlBuilder func(operation *openapi2.Operation, method string, scheme, host, basePath, path, query string) string
	scheme     string
	headers    func(ctx context.Context) (map[string]string, error)
}

func (o *OpenApiTools) PrintOperationIds() {
	for _, path := range o.doc.Paths {
		for _, operation := range path.Operations() {
			log.Printf(" OperationId: %s\n", operation.OperationID)
		}
	}
}

type option func(*OpenApiTools)

// WithHost allows you to set a custom host for the OpenApiTools instance.
func WithHost(host string) option {
	return func(o *OpenApiTools) {
		o.doc.Host = host
	}
}

// WithBasePath allows you to set a custom base path for the OpenApiTools instance.
func WithBasePath(basePath string) option {
	return func(o *OpenApiTools) {
		o.doc.BasePath = basePath
	}
}

// LoadFromURL loads an OpenAPI specification from a URL and applies it to the OpenApiTools instance.
func LoadFromURL(urlStr string) option {
	return func(o *OpenApiTools) {
		resp, err := http.Get(urlStr)
		if err != nil {
			glog.Errorf("Error fetching OpenAPI spec from URL: %v", err)
			return
		}
		defer resp.Body.Close()

		if err = json.NewDecoder(resp.Body).Decode(&o.doc); err != nil {
			glog.Errorf("Error decoding OpenAPI spec from URL: %v", err)
		}
	}
}

// LoadFromFile loads an OpenAPI specification from a local file and applies it to the OpenApiTools instance.
func LoadFromFile(path string) option {
	return func(o *OpenApiTools) {
		data, err := os.ReadFile(path)
		if err != nil {
			glog.Errorf("Error reading OpenAPI spec from file: %v", err)
			return
		}

		if err = json.Unmarshal(data, &o.doc); err != nil {
			glog.Errorf("Error decoding OpenAPI spec from file: %v", err)
		}
	}
}

// FilterByMethods filters the OpenAPI specification to only include operations that use the specified HTTP methods.
func FilterByMethods(methods []string) option {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		allowed[strings.ToLower(method)] = struct{}{}
	}

	return func(o *OpenApiTools) {
		for _, path := range o.doc.Paths {
			for method := range path.Operations() {
				if _, ok := allowed[strings.ToLower(method)]; !ok {
					path.SetOperation(method, nil)
				}
			}
		}
	}
}

// FilterByTags filters the OpenAPI specification to only include operations that have at least one of the specified tags.
func FilterByTags(tags []string) option {
	allowed := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		allowed[tag] = struct{}{}
	}

	hasAllowedTag := func(tags []string) bool {
		for _, tag := range tags {
			if _, ok := allowed[tag]; ok {
				return true
			}
		}
		return false
	}

	return func(o *OpenApiTools) {
		for _, path := range o.doc.Paths {
			for method, operation := range path.Operations() {
				if !hasAllowedTag(operation.Tags) {
					path.SetOperation(method, nil)
				}
			}
		}
	}
}

// FilterByPaths filters the OpenAPI specification to only include paths that match the specified paths.
func FilterByPaths(paths []string) option {
	allowed := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		allowed[path] = struct{}{}
	}

	return func(o *OpenApiTools) {
		for pathStr := range o.doc.Paths {
			if _, ok := allowed[pathStr]; !ok {
				delete(o.doc.Paths, pathStr)
			}
		}
	}
}

// FilterByOperationIds filters the OpenAPI specification to only include operations with the specified operation IDs.
func FilterByOperationIds(operationIds []string) option {
	allowed := make(map[string]struct{}, len(operationIds))
	for _, operationId := range operationIds {
		allowed[operationId] = struct{}{}
	}

	return func(o *OpenApiTools) {
		for _, path := range o.doc.Paths {
			for method, operation := range path.Operations() {
				if _, ok := allowed[operation.OperationID]; !ok {
					path.SetOperation(method, nil)
				}
			}

		}
	}
}

// WithUrlBuilder allows you to set a custom URL builder function for the OpenApiTools instance.
// This can be useful for customizing how URLs are constructed when making requests to the API.
// Example usage:
//
//	o := NewOpenApiSpec(
//		LoadFromURL("https://example.com/api-docs.json"),
//		WithUrlBuilder(func(scheme, host, basePath, path, query string) string {
//			base := scheme + "://" + host + basePath + "/custom" + path
//			if query != "" {
//				base += "?" + query
//			}
//			return base
//		}),
//
// )
func WithUrlBuilder(urlBuilder func(operation *openapi2.Operation, method string, scheme, host, basePath, path, query string) string) option {
	return func(o *OpenApiTools) {
		o.urlBuilder = urlBuilder
	}
}

// WithHeaders allows you to set custom headers for the OpenApiTools instance.
// This can be useful for adding authentication tokens or other necessary headers when making requests to the API.
// Example usage:
//
//	o := NewOpenApiSpec(
//		LoadFromURL("https://example.com/api-docs.json"),
//		WithHeaders(func(ctx context.Context) map[string]string {
//	    	return map[string]string{
//	    		"Authorization": "Bearer <token>",
//	    		"Content-Type":  "application/json",
//	    	}
//		}),
//	)
func WithHeaders(provider func(ctx context.Context) (map[string]string, error)) option {
	return func(o *OpenApiTools) {
		o.headers = provider
	}
}

// GetDoc returns the OpenAPI specification document associated with the OpenApiTools instance.
func (o *OpenApiTools) GetDoc() *openapi2.T {
	return o.doc
}

func NewOpenApiSpec(opts ...option) (*OpenApiTools, error) {
	o := &OpenApiTools{
		scheme: "https",
	}

	for _, apply := range opts {
		apply(o)
	}

	if len(o.doc.Schemes) > 0 {
		o.scheme = o.doc.Schemes[0]
	}

	return o, nil
}

// RegisterToMcpServer registers the OpenApiTools instance to the provided MCPServer.
// It builds the tools from the OpenAPI specification and adds them to the server.
// If there is an error during the tool building process, it returns the error.
func (o *OpenApiTools) RegisterToMcpServer(server *server.MCPServer) error {
	tools, err := o.buildTools()
	if err != nil {
		return err
	}

	log.Printf("%d tools registered in %s", len(tools), o.doc.Info.Title)
	server.AddTools(tools...)
	return nil
}
