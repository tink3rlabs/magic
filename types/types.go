package types

// @openapi
// components:
//
//	responses:
//	  BadRequest:
//	    description: The request is invalid
//	    content:
//	      application/json:
//	        schema:
//	          $ref: "#/components/schemas/Error"
//	        example:
//	          status: Bad Request
//	          error: request validation faild
//	          details: ["(root): Additional property foo is not allowed", "bar: Invalid type. Expected: string, given: integer"]
//	  Unauthorized:
//	    description: The request lacks valid authentication credentials
//	    content:
//	      application/json:
//	        schema:
//	          $ref: "#/components/schemas/Error"
//	        example:
//	          status: Unauthorized
//	          error: "invalid authentication token: token expired"
//	  Forbidden:
//	    description: Insufficient permissions to a resource or action
//	    content:
//	      application/json:
//	        schema:
//	          $ref: "#/components/schemas/Error"
//	        example:
//	          status: Forbidden
//	          error: "you are not allowed to perform this action on this resource"
//	  NotFound:
//	    description: The specified resource was not found
//	    content:
//	      application/json:
//	        schema:
//	          $ref: "#/components/schemas/Error"
//	        example:
//	          status: Not Found
//	          error: "the requested resources was not found"
//	  ServerError:
//	    description: There was an unexpected server error
//	    content:
//	      application/json:
//	        schema:
//	          $ref: "#/components/schemas/Error"
//	        example:
//	          status: Internal Server Error
//	          error: "encountered an unexpected server error: the server couldn't process this request"
//	schemas:
//	  Error:
//	    type: object
//	    properties:
//	      status:
//	        type: string
//	      error:
//	        type: string
//	      details:
//	        type: array
//	        items:
//	          type: string
type ErrorResponse struct {
	Status  string   `json:"status"`
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

// @openapi
// components:
//
//	schemas:
//	  PatchBody:
//	    type: object
//	    description: A JSONPatch document as defined by RFC 6902
//	    additionalProperties: false
//	    required:
//	      - op
//	      - path
//	    properties:
//	      op:
//	        type: string
//	        description: The operation to be performed
//	        enum:
//	          - add
//	          - remove
//	          - replace
//	          - move
//	          - copy
//	          - test
//	      path:
//	        type: string
//	        description: A JSON-Pointer
//	      value:
//	        description: The value to be used within the operations.
//	      from:
//	        type: string
//	        description: A string containing a JSON Pointer value.
type PatchBody struct{}
