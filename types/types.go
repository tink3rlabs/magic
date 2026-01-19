package types

import (
	"encoding/json"

	"github.com/TwiN/deepmerge"
)

const definitions = `
{
	"components": {
		"responses": {
		"BadRequest": {
			"description": "The request is invalid",
			"content": {
			"application/json": {
				"schema": {
				"$ref": "#/components/schemas/Error"
				},
				"example": {
				"status": "Bad Request",
				"error": "request validation failed",
				"details": [
					"(root): Additional property foo is not allowed",
					"bar: Invalid type. Expected: string, given: integer"
				]
				}
			}
			}
		},
		"Unauthorized": {
			"description": "The request lacks valid authentication credentials",
			"content": {
			"application/json": {
				"schema": {
				"$ref": "#/components/schemas/Error"
				},
				"example": {
				"status": "Unauthorized",
				"error": "invalid authentication token: token expired"
				}
			}
			}
		},
		"Forbidden": {
			"description": "Insufficient permissions to a resource or action",
			"content": {
			"application/json": {
				"schema": {
				"$ref": "#/components/schemas/Error"
				},
				"example": {
				"status": "Forbidden",
				"error": "you are not allowed to perform this action on this resource"
				}
			}
			}
		},
		"NotFound": {
			"description": "The specified resource was not found",
			"content": {
			"application/json": {
				"schema": {
				"$ref": "#/components/schemas/Error"
				},
				"example": {
				"status": "Not Found",
				"error": "the requested resources was not found"
				}
			}
			}
		},
		"ServerError": {
			"description": "There was an unexpected server error",
			"content": {
			"application/json": {
				"schema": {
				"$ref": "#/components/schemas/Error"
				},
				"example": {
				"status": "Internal Server Error",
				"error": "encountered an unexpected server error: the server couldn't process this request"
				}
			}
			}
		}
		},
		"schemas": {
		"Error": {
			"type": "object",
			"properties": {
			"status": {
				"type": "string"
			},
			"error": {
				"type": "string"
			},
			"details": {
				"type": "array",
				"items": {
				"type": "string"
				}
			}
			}
		},
		"PatchBody": {
			"type": "object",
			"description": "A JSONPatch document as defined by RFC 6902",
			"additionalProperties": false,
			"required": [
			"op",
			"path"
			],
			"properties": {
			"op": {
				"type": "string",
				"description": "The operation to be performed",
				"enum": [
				"add",
				"remove",
				"replace",
				"move",
				"copy",
				"test"
				]
			},
			"path": {
				"type": "string",
				"description": "A JSON-Pointer"
			},
			"value": {
				"description": "The value to be used within the operations."
			},
			"from": {
				"type": "string",
				"description": "A string containing a JSON Pointer value."
			}
			}
		},
		"LuceneSearchQuery": {
			"type": "string",
			"description": "Lucene-style search query supporting field searches, wildcards, boolean operators, ranges, and more. Syntax: field:value, wildcards (*,?), operators (AND, OR, NOT, +, -), ranges ([min TO max]), quoted phrases, JSONB access (field.subfield:value), null checks (field:null), and fuzzy search (term~). Examples: name:john, name:john*, email:*@example.com, description:*important*, name:john* OR email:*@example.com, name:john AND status:active, status:active OR status:pending, name:john NOT status:inactive, +name:john +status:active, name:john -status:deleted, age:[25 TO 65], age:{25 TO 65}, age:[25 TO *], age:[* TO 65], created_at:[2024-01-01 TO 2024-12-31], description:\"hello world\", title:\"test-app (v1.0)\", name:C\\+\\+ OR path:\\/usr\\/bin, (name:john* OR email:*@example.com) AND status:active AND age:[25 TO 65], ((name:john OR name:jane) AND status:active) OR (status:pending AND age:[18 TO *]), searchterm, john*, labels.category:production, metadata.tags:prod*, name:john AND labels.env:prod AND metadata.team:engineering, parent_id:null, NOT deleted_at:null, name:john AND deleted_at:null, name:roam~, name:roam~2, labels.tag:prod~, +name:john* -status:deleted age:[25 TO 65] AND (role:admin OR role:moderator), name:john OR email:john@example.com OR phone:*555*, (name:*admin* OR role:administrator) AND status:active AND NOT deleted_at:null AND created_at:[2024-01-01 TO *]",
			"example": "name:john AND status:active"
		}
	}
	}
}
`

type ErrorResponse struct {
	Status  string   `json:"status"`
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

func GetOpenAPIDefinitions() ([]byte, error) {
	var data map[string]interface{}
	err := json.Unmarshal([]byte(definitions), &data)
	return []byte(definitions), err
}

func MergeOpenAPIDefinitions(inputDefinition []byte) ([]byte, error) {
	def, err := GetOpenAPIDefinitions()
	if err != nil {
		return nil, err
	}

	combined, err := deepmerge.JSON(inputDefinition, def)
	if err != nil {
		return nil, err
	}

	return combined, nil
}
