package openapi_mcpgo

type HeaderProvider interface {
	GetHeaders() map[string]string
}

type StaticHeaderProvider map[string]string

func (s StaticHeaderProvider) GetHeaders() map[string]string {
	return s
}

type DynamicHeaderProvider func() map[string]string

func (d DynamicHeaderProvider) GetHeaders() map[string]string {
	return d()
}
