package openapi_mcpgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
)

type OpenApiTools struct {
	spec *spec
}

func (o *OpenApiTools) validate() error {
	if o.spec == nil {
		return fmt.Errorf("spec is nil. ensure you load a spec using LoadFromFile or LoadFromURL")
	}
	return nil
}

type option func(*OpenApiTools)

func FilterByHTTPMethodDetails(key, value string) option {
	return func(o *OpenApiTools) {
		for _, pathDetails := range o.spec.Paths {
			for method, methodDetails := range pathDetails {
				if methodDetails[key] == value {
					continue
				}
				delete(pathDetails, method)
			}

		}
	}
}

func FilterByHTTPMethods(ms []string) option {
	return func(o *OpenApiTools) {
		for _, pathDetails := range o.spec.Paths {
			for method := range pathDetails {
				safeMethod := strings.ToLower(method)

				if slices.Contains(ms, safeMethod) {
					continue
				}

				delete(pathDetails, method)
			}
		}
	}

}

func LoadFromFile(path string) option {
	var s spec

	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &s)
	}

	return func(o *OpenApiTools) {
		if err == nil {
			o.spec = &s
		}
	}
}

func LoadFromURL(url string) option {
	var s spec

	resp, err := http.Get(url)
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&s)
	}

	return func(o *OpenApiTools) {
		if err == nil {
			o.spec = &s
		}
	}
}

func NewOpenApiSpec(opts ...option) (*OpenApiTools, error) {
	o := &OpenApiTools{}
	for _, apply := range opts {
		apply(o)
	}

	if err := o.validate(); err != nil {
		return nil, err
	}

	return o, nil
}
