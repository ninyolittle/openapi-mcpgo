package openapi_mcpgo

type spec struct {
	Swagger  string              `json:"swagger"`
	Tags     []map[string]string `json:"tags"`
	Info     map[string]string   `json:"info"`
	Host     string              `json:"host"`
	BasePath string              `json:"basePath"`
	Paths    Paths               `json:"paths"`
}

type Paths map[string]map[string]map[string]any
