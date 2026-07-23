// MIT License
//
// Copyright (c) 2026 GoAkt Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

package schemaconv

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/portcullis/mcp"
)

// SDKResourcesToSchemas converts a slice of SDK Resource pointers into gateway
// ResourceSchema values.
func SDKResourcesToSchemas(resources []*sdkmcp.Resource) []mcp.ResourceSchema {
	if len(resources) == 0 {
		return nil
	}
	schemas := make([]mcp.ResourceSchema, 0, len(resources))
	for _, r := range resources {
		if r == nil {
			continue
		}
		schemas = append(schemas, mcp.ResourceSchema{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MIMEType:    r.MIMEType,
		})
	}
	return schemas
}

// SDKResourceTemplatesToSchemas converts a slice of SDK ResourceTemplate pointers
// into gateway ResourceTemplateSchema values.
func SDKResourceTemplatesToSchemas(templates []*sdkmcp.ResourceTemplate) []mcp.ResourceTemplateSchema {
	if len(templates) == 0 {
		return nil
	}
	schemas := make([]mcp.ResourceTemplateSchema, 0, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		schemas = append(schemas, mcp.ResourceTemplateSchema{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Description: t.Description,
			MIMEType:    t.MIMEType,
		})
	}
	return schemas
}
