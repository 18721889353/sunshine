package parser

import (
	"sync"
	"text/template"

	"github.com/pkg/errors"
)

var (
	modelStructTmpl    *template.Template
	modelStructTmplRaw = `
{{- if .Comment -}}
// {{.TableName}} {{.Comment}}
{{end -}}
type {{.TableName}} struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}} {{if .Tag}}` + "`{{.Tag}}`" + `{{end}}{{if .Comment}} // {{.Comment}}{{end}}
{{- end}}
}
{{if .NameFunc}}
// TableName table name
func (m *{{.TableName}}) TableName() string {
	return "{{.RawTableName}}"
}
{{end}}
`

	modelTmpl    *template.Template
	modelTmplRaw = `package {{.Package}}
{{if .ImportPath}}
import (
	{{- range .ImportPath}}
	"{{.}}"
	{{- end}}
)
{{- end}}
{{range .StructCode}}
{{.}}
{{end}}`

	updateFieldTmpl    *template.Template
	updateFieldTmplRaw = `
{{- range .Fields}}
	if table.{{.Name}} {{.ConditionZero}} {
		update["{{.ColName}}"] = {{.DerefExpr "table"}}
	}
{{- end}}`

	handlerCreateStructTmpl    *template.Template
	handlerCreateStructTmplRaw = `
// Create{{.TableName}}Request request params
type Create{{.TableName}}Request struct {
{{- range .Fields}}
	{{.Name}}  {{.GoType}} ` + "`" + `json:"{{.JSONName}}" binding:""` + "`" + `{{if .Comment}} // {{.Comment}}{{end}}
{{- end}}
}
`

	handlerUpdateStructTmpl    *template.Template
	handlerUpdateStructTmplRaw = `
// Update{{.TableName}}ByIDRequest request params
type Update{{.TableName}}ByIDRequest struct {
{{- range .Fields}}
	{{.Name}}  {{.GoType}} ` + "`" + `json:"{{.JSONName}}" binding:""` + "`" + `{{if .Comment}} // {{.Comment}}{{end}}
{{- end}}
}
`

	handlerDetailStructTmpl    *template.Template
	handlerDetailStructTmplRaw = `
// {{.TableName}}ObjDetail detail
type {{.TableName}}ObjDetail struct {
{{- range .Fields}}
	{{.Name}}  {{.GoType}} ` + "`" + `json:"{{.JSONName}}"` + "`" + `{{if .Comment}} // {{.Comment}}{{end}}
{{- end}}
}`

	modelJSONTmpl    *template.Template
	modelJSONTmplRaw = `{
{{- range .Fields}}
	"{{.ColName}}" {{.GoZero}}
{{- end}}
}
`

	protoFileTmpl    *template.Template
	protoFileTmplRaw = `syntax = "proto3";

package api.serverNameExample.v1;

import "api/types/types.proto";
import "validate/validate.proto";

option go_package = "github.com/18721889353/sunshine/api/serverNameExample/v1;v1";

service {{.TName}} {
  // create {{.TName}}
  rpc Create(Create{{.TableName}}Request) returns (Create{{.TableName}}Reply) {}

  // delete {{.TName}} by id
  rpc DeleteByID(Delete{{.TableName}}ByIDRequest) returns (Delete{{.TableName}}ByIDReply) {}

  // update {{.TName}} by id
  rpc UpdateByID(Update{{.TableName}}ByIDRequest) returns (Update{{.TableName}}ByIDReply) {}

  // get {{.TName}} by id
  rpc GetByID(Get{{.TableName}}ByIDRequest) returns (Get{{.TableName}}ByIDReply) {}

  // list of {{.TName}} by query parameters
  rpc List(List{{.TableName}}Request) returns (List{{.TableName}}Reply) {}

  // delete {{.TName}} by batch id
  rpc DeleteByIDs(Delete{{.TableName}}ByIDsRequest) returns (Delete{{.TableName}}ByIDsReply) {}

  // get {{.TName}} by condition
  rpc GetByCondition(Get{{.TableName}}ByConditionRequest) returns (Get{{.TableName}}ByConditionReply) {}

  // list of {{.TName}} by batch id
  rpc ListByIDs(List{{.TableName}}ByIDsRequest) returns (List{{.TableName}}ByIDsReply) {}

  // list {{.TName}} by last id
  rpc ListByLastID(List{{.TableName}}ByLastIDRequest) returns (List{{.TableName}}ByLastIDReply) {}
}


/*
Notes for defining message fields:
    1. Suggest using camel case style naming for message field names, such as firstName, lastName, etc.
    2. If the message field name ending in 'id', it is recommended to use xxxID naming format, such as userID, orderID, etc.
    3. Add validate rules https://github.com/envoyproxy/protoc-gen-validate#constraint-rules, such as:
        uint64 id = 1 [(validate.rules).uint64.gte  = 1];
*/


// protoMessageCreateCode

message Create{{.TableName}}Reply {
  // createTableReplyFieldCode
}

message Delete{{.TableName}}ByIDRequest {
  // deleteTableByIDRequestFieldCode
}

message Delete{{.TableName}}ByIDReply {

}

// protoMessageUpdateCode

message Update{{.TableName}}ByIDReply {

}

// protoMessageDetailCode

message Get{{.TableName}}ByIDRequest {
  // getTableByIDRequestFieldCode
}

message Get{{.TableName}}ByIDReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}Request {
  api.types.Params params = 1;
}

message List{{.TableName}}Reply {
  int64 total = 1;
  repeated {{.TableName}} {{.TName}}s = 2;
}

message Delete{{.TableName}}ByIDsRequest {
  // deleteTableByIDsRequestFieldCode
}

message Delete{{.TableName}}ByIDsReply {

}

message Get{{.TableName}}ByConditionRequest {
  types.Conditions conditions = 1;
}

message Get{{.TableName}}ByConditionReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}ByIDsRequest {
  // getTableByIDsRequestFieldCode
}

message List{{.TableName}}ByIDsReply {
  repeated {{.TableName}} {{.TName}}s = 1;
}

message List{{.TableName}}ByLastIDRequest {
  // listTableByLastIDRequestFieldCode
  uint32 limit = 2 [(validate.rules).uint32.gt = 0]; // limit size per page
  string sort = 3; // sort by column name of table, default is -id, the - sign indicates descending order.
}

message List{{.TableName}}ByLastIDReply {
  repeated {{.TableName}} {{.TName}}s = 1;
}
`

	protoFileSimpleTmpl    *template.Template
	protoFileSimpleTmplRaw = `syntax = "proto3";

package api.serverNameExample.v1;

import "api/types/types.proto";
import "validate/validate.proto";

option go_package = "github.com/18721889353/sunshine/api/serverNameExample/v1;v1";

service {{.TName}} {
  // create {{.TName}}
  rpc Create(Create{{.TableName}}Request) returns (Create{{.TableName}}Reply) {}

  // delete {{.TName}} by id
  rpc DeleteByID(Delete{{.TableName}}ByIDRequest) returns (Delete{{.TableName}}ByIDReply) {}

  // update {{.TName}} by id
  rpc UpdateByID(Update{{.TableName}}ByIDRequest) returns (Update{{.TableName}}ByIDReply) {}

  // get {{.TName}} by id
  rpc GetByID(Get{{.TableName}}ByIDRequest) returns (Get{{.TableName}}ByIDReply) {}

  // list of {{.TName}} by query parameters
  rpc List(List{{.TableName}}Request) returns (List{{.TableName}}Reply) {}
}


/*
Notes for defining message fields:
    1. Suggest using camel case style naming for message field names, such as firstName, lastName, etc.
    2. If the message field name ending in 'id', it is recommended to use xxxID naming format, such as userID, orderID, etc.
    3. Add validate rules https://github.com/envoyproxy/protoc-gen-validate#constraint-rules, such as:
        uint64 id = 1 [(validate.rules).uint64.gte  = 1];
*/


// protoMessageCreateCode

message Create{{.TableName}}Reply {
  // createTableReplyFieldCode
}

message Delete{{.TableName}}ByIDRequest {
  // deleteTableByIDRequestFieldCode
}

message Delete{{.TableName}}ByIDReply {

}

// protoMessageUpdateCode

message Update{{.TableName}}ByIDReply {

}

// protoMessageDetailCode

message Get{{.TableName}}ByIDRequest {
  // getTableByIDRequestFieldCode
}

message Get{{.TableName}}ByIDReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}Request {
  api.types.Params params = 1;
}

message List{{.TableName}}Reply {
  int64 total = 1;
  repeated {{.TableName}} {{.TName}}s = 2;
}
`

	protoFileForWebTmpl    *template.Template
	protoFileForWebTmplRaw = `syntax = "proto3";

package api.serverNameExample.v1;

import "api/types/types.proto";
import "google/api/annotations.proto";
import "protoc-gen-openapiv2/options/annotations.proto";
import "tagger/tagger.proto";
import "validate/validate.proto";

option go_package = "github.com/18721889353/sunshine/api/serverNameExample/v1;v1";

/*
Reference https://github.com/grpc-ecosystem/grpc-gateway/blob/db7fbefff7c04877cdb32e16d4a248a024428207/examples/internal/proto/examplepb/a_bit_of_everything.proto
Default settings for generating swagger documents
NOTE: because json does not support 64 bits, the int64 and uint64 types under *.swagger.json are automatically converted to string types
Tips: add swagger option to rpc method, example:
    option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
      summary: "get user by id",
      description: "get user by id",
      security: {
        security_requirement: {
          key: "BearerAuth";
          value: {}
        }
      }
    };
*/
option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger) = {
  host: "localhost:8080"
  base_path: ""
  info: {
    title: "serverNameExample api docs";
    version: "2.0";
  }
  schemes: HTTP;
  schemes: HTTPS;
  consumes: "application/json";
  produces: "application/json";
  security_definitions: {
    security: {
      key: "BearerAuth";
      value: {
        type: TYPE_API_KEY;
        in: IN_HEADER;
        name: "Authorization";
        description: "Type Bearer your-jwt-token to Value";
      }
    }
  }
};

service {{.TName}} {
  // create {{.TName}}
  rpc Create(Create{{.TableName}}Request) returns (Create{{.TableName}}Reply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}"
      body: "*"
    };
  }

  // delete {{.TName}} by id
  rpc DeleteByID(Delete{{.TableName}}ByIDRequest) returns (Delete{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      delete: "/api/v1/{{.TName}}/{id}"
    };
  }

  // update {{.TName}} by id
  rpc UpdateByID(Update{{.TableName}}ByIDRequest) returns (Update{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      put: "/api/v1/{{.TName}}/{id}"
      body: "*"
    };
  }

  // get {{.TName}} by id
  rpc GetByID(Get{{.TableName}}ByIDRequest) returns (Get{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      get: "/api/v1/{{.TName}}/{id}"
    };
  }

  // list of {{.TName}} by query parameters
  rpc List(List{{.TableName}}Request) returns (List{{.TableName}}Reply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}/list"
      body: "*"
    };
  }

  // delete {{.TName}} by batch id
  rpc DeleteByIDs(Delete{{.TableName}}ByIDsRequest) returns (Delete{{.TableName}}ByIDsReply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}/delete/ids"
      body: "*"
    };
  }

  // get {{.TName}} by condition
  rpc GetByCondition(Get{{.TableName}}ByConditionRequest) returns (Get{{.TableName}}ByConditionReply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}/condition"
      body: "*"
    };
  }

  // list of {{.TName}} by batch id
  rpc ListByIDs(List{{.TableName}}ByIDsRequest) returns (List{{.TableName}}ByIDsReply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}/list/ids"
      body: "*"
    };
  }

  // list {{.TName}} by last id
  rpc ListByLastID(List{{.TableName}}ByLastIDRequest) returns (List{{.TableName}}ByLastIDReply) {
    option (google.api.http) = {
      get: "/api/v1/{{.TName}}/list"
    };
  }
}


/*
Notes for defining message fields:
    1. Suggest using camel case style naming for message field names, such as firstName, lastName, etc.
    2. If the message field name ending in 'id', it is recommended to use xxxID naming format, such as userID, orderID, etc.
    3. Add validate rules https://github.com/envoyproxy/protoc-gen-validate#constraint-rules, such as:
        uint64 id = 1 [(validate.rules).uint64.gte  = 1];

If used to generate code that supports the HTTP protocol, notes for defining message fields:
    1. If the route contains the path parameter, such as /api/v1/userExample/{id}, the defined
        message must contain the name of the path parameter and the name should be added
        with a new tag, such as int64 id = 1 [(tagger.tags) = "uri:\"id\""];
    2. If the request url is followed by a query parameter, such as /api/v1/getUserExample?name=Tom,
        a form tag must be added when defining the query parameter in the message, such as:
        string name = 1 [(tagger.tags) = "form:\"name\""].
    3. If the message field name contain underscores(such as 'field_name'), it will cause a problem
        where the JSON field names of the Swagger request parameters are different from those of the
        GRPC JSON tag names. There are two solutions: Solution 1, remove the underline from the
         message field name. Option 2, use the tool 'protoc-go-inject-tag' to modify the JSON tag name,
         such as: string first_name = 1 ; // @gotags: json:"firstName"
*/


// protoMessageCreateCode

message Create{{.TableName}}Reply {
  // createTableReplyFieldCode
}

message Delete{{.TableName}}ByIDRequest {
  // deleteTableByIDRequestFieldCode
}

message Delete{{.TableName}}ByIDReply {

}

// protoMessageUpdateCode

message Update{{.TableName}}ByIDReply {

}

// protoMessageDetailCode

message Get{{.TableName}}ByIDRequest {
  // getTableByIDRequestFieldCode
}

message Get{{.TableName}}ByIDReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}Request {
  api.types.Params params = 1;
}

message List{{.TableName}}Reply {
  int64 total = 1;
  repeated {{.TableName}} {{.TName}}s = 2;
}

message Delete{{.TableName}}ByIDsRequest {
  // deleteTableByIDsRequestFieldCode
}

message Delete{{.TableName}}ByIDsReply {

}

message Get{{.TableName}}ByConditionRequest {
  types.Conditions conditions = 1;
}

message Get{{.TableName}}ByConditionReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}ByIDsRequest {
  // getTableByIDsRequestFieldCode
}

message List{{.TableName}}ByIDsReply {
  repeated {{.TableName}} {{.TName}}s = 1;
}

message List{{.TableName}}ByLastIDRequest {
  // listTableByLastIDRequestFieldCode
  uint32 limit = 2 [(validate.rules).uint32.gt = 0, (tagger.tags) = "form:\"limit\""]; // limit size per page
  string sort = 3 [(tagger.tags) = "form:\"sort\""]; // sort by column name of table, default is -id, the - sign indicates descending order.
}

message List{{.TableName}}ByLastIDReply {
  repeated {{.TableName}} {{.TName}}s = 1;
}
`

	protoFileForSimpleWebTmpl    *template.Template
	protoFileForSimpleWebTmplRaw = `syntax = "proto3";

package api.serverNameExample.v1;

import "api/types/types.proto";
import "google/api/annotations.proto";
import "protoc-gen-openapiv2/options/annotations.proto";
import "tagger/tagger.proto";
import "validate/validate.proto";

option go_package = "github.com/18721889353/sunshine/api/serverNameExample/v1;v1";

/*
Reference https://github.com/grpc-ecosystem/grpc-gateway/blob/db7fbefff7c04877cdb32e16d4a248a024428207/examples/internal/proto/examplepb/a_bit_of_everything.proto
Default settings for generating swagger documents
NOTE: because json does not support 64 bits, the int64 and uint64 types under *.swagger.json are automatically converted to string types
Tips: add swagger option to rpc method, example:
    option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
      summary: "get user by id",
      description: "get user by id",
      security: {
        security_requirement: {
          key: "BearerAuth";
          value: {}
        }
      }
    };
*/
option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger) = {
  host: "localhost:8080"
  base_path: ""
  info: {
    title: "serverNameExample api docs";
    version: "2.0";
  }
  schemes: HTTP;
  schemes: HTTPS;
  consumes: "application/json";
  produces: "application/json";
  security_definitions: {
    security: {
      key: "BearerAuth";
      value: {
        type: TYPE_API_KEY;
        in: IN_HEADER;
        name: "Authorization";
        description: "Type Bearer your-jwt-token to Value";
      }
    }
  }
};

service {{.TName}} {
  // create {{.TName}}
  rpc Create(Create{{.TableName}}Request) returns (Create{{.TableName}}Reply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}"
      body: "*"
    };
  }

  // delete {{.TName}} by id
  rpc DeleteByID(Delete{{.TableName}}ByIDRequest) returns (Delete{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      delete: "/api/v1/{{.TName}}/{id}"
    };
  }

  // update {{.TName}} by id
  rpc UpdateByID(Update{{.TableName}}ByIDRequest) returns (Update{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      put: "/api/v1/{{.TName}}/{id}"
      body: "*"
    };
  }

  // get {{.TName}} by id
  rpc GetByID(Get{{.TableName}}ByIDRequest) returns (Get{{.TableName}}ByIDReply) {
    option (google.api.http) = {
      get: "/api/v1/{{.TName}}/{id}"
    };
  }

  // list of {{.TName}} by query parameters
  rpc List(List{{.TableName}}Request) returns (List{{.TableName}}Reply) {
    option (google.api.http) = {
      post: "/api/v1/{{.TName}}/list"
      body: "*"
    };
  }
}


/*
Notes for defining message fields:
    1. Suggest using camel case style naming for message field names, such as firstName, lastName, etc.
    2. If the message field name ending in 'id', it is recommended to use xxxID naming format, such as userID, orderID, etc.
    3. Add validate rules https://github.com/envoyproxy/protoc-gen-validate#constraint-rules, such as:
        uint64 id = 1 [(validate.rules).uint64.gte  = 1];

If used to generate code that supports the HTTP protocol, notes for defining message fields:
    1. If the route contains the path parameter, such as /api/v1/userExample/{id}, the defined
        message must contain the name of the path parameter and the name should be added
        with a new tag, such as int64 id = 1 [(tagger.tags) = "uri:\"id\""];
    2. If the request url is followed by a query parameter, such as /api/v1/getUserExample?name=Tom,
        a form tag must be added when defining the query parameter in the message, such as:
        string name = 1 [(tagger.tags) = "form:\"name\""].
    3. If the message field name contain underscores(such as 'field_name'), it will cause a problem
        where the JSON field names of the Swagger request parameters are different from those of the
        GRPC JSON tag names. There are two solutions: Solution 1, remove the underline from the
         message field name. Option 2, use the tool 'protoc-go-inject-tag' to modify the JSON tag name,
         such as: string first_name = 1 ; // @gotags: json:"firstName"
*/


// protoMessageCreateCode

message Create{{.TableName}}Reply {
  // createTableReplyFieldCode
}

message Delete{{.TableName}}ByIDRequest {
  // deleteTableByIDRequestFieldCode
}

message Delete{{.TableName}}ByIDReply {

}

// protoMessageUpdateCode

message Update{{.TableName}}ByIDReply {

}

// protoMessageDetailCode

message Get{{.TableName}}ByIDRequest {
  // getTableByIDRequestFieldCode
}

message Get{{.TableName}}ByIDReply {
  {{.TableName}} {{.TName}} = 1;
}

message List{{.TableName}}Request {
  api.types.Params params = 1;
}

message List{{.TableName}}Reply {
  int64 total = 1;
  repeated {{.TableName}} {{.TName}}s = 2;
}
`

	protoMessageCreateTmpl    *template.Template
	protoMessageCreateTmplRaw = `message Create{{.TableName}}Request {
{{- range $i, $v := .Fields}}
	{{$v.GoType}} {{$v.JSONName}} = {{$v.AddOne $i}}; {{if $v.Comment}} // {{$v.Comment}}{{end}}
{{- end}}
}`

	protoMessageUpdateTmpl    *template.Template
	protoMessageUpdateTmplRaw = `message Update{{.TableName}}ByIDRequest {
{{- range $i, $v := .Fields}}
	{{$v.GoType}} {{$v.JSONName}} = {{$v.AddOneWithTag $i}}; {{if $v.Comment}} // {{$v.Comment}}{{end}}
{{- end}}
}`

	protoMessageDetailTmpl    *template.Template
	protoMessageDetailTmplRaw = `message {{.TableName}} {
{{- range $i, $v := .Fields}}
	{{$v.GoType}} {{$v.JSONName}} = {{$v.AddOne $i}}; {{if $v.Comment}} // {{$v.Comment}}{{end}}
{{- end}}
}`

	serviceStructTmpl    *template.Template
	serviceStructTmplRaw = `
		{
			name: "Create",
			fn: func() (interface{}, error) {
				// todo enter parameters before testing
// serviceCreateStructCode
			},
			wantErr: false,
		},

		{
			name: "UpdateByID",
			fn: func() (interface{}, error) {
				// todo enter parameters before testing
// serviceUpdateStructCode
			},
			wantErr: false,
		},
`

	serviceCreateStructTmpl    *template.Template
	serviceCreateStructTmplRaw = `				req := &serverNameExampleV1.Create{{.TableName}}Request{
					{{- range .Fields}}
						{{.Name}}:  {{.GoTypeZero}}, {{if .Comment}} // {{.Comment}}{{end}}
					{{- end}}
				}
				return cli.Create(ctx, req)`

	serviceUpdateStructTmpl    *template.Template
	serviceUpdateStructTmplRaw = `				req := &serverNameExampleV1.Update{{.TableName}}ByIDRequest{
					{{- range .Fields}}
						{{.Name}}:  {{.GoTypeZero}}, {{if .Comment}} // {{.Comment}}{{end}}
					{{- end}}
				}
				return cli.UpdateByID(ctx, req)`

	tmplParseOnce sync.Once
)

// parseTemplate 解析单个模板
func parseTemplate(name string, tmplRaw string, existingErr error) (*template.Template, error) {
	tmpl, err := template.New(name).Parse(tmplRaw)
	if err != nil {
		if existingErr != nil {
			return nil, errors.Wrap(existingErr, name+":"+err.Error())
		}
		return nil, errors.Wrap(err, name)
	}
	return tmpl, nil
}

// templateConfig 模板配置结构
type templateConfig struct {
	name    string
	raw     **template.Template
	tmplRaw string
}

// parseAllTemplates 解析所有模板
func parseAllTemplates() error {
	var errSum error

	templates := []templateConfig{
		{"goStruct", &modelStructTmpl, modelStructTmplRaw},
		{"goFile", &modelTmpl, modelTmplRaw},
		{"goUpdateField", &updateFieldTmpl, updateFieldTmplRaw},
		{"goPostStruct", &handlerCreateStructTmpl, handlerCreateStructTmplRaw},
		{"goPutStruct", &handlerUpdateStructTmpl, handlerUpdateStructTmplRaw},
		{"goGetStruct", &handlerDetailStructTmpl, handlerDetailStructTmplRaw},
		{"modelJSON", &modelJSONTmpl, modelJSONTmplRaw},
		{"protoFile", &protoFileTmpl, protoFileTmplRaw},
		{"protoFileSimple", &protoFileSimpleTmpl, protoFileSimpleTmplRaw},
		{"protoFileForWeb", &protoFileForWebTmpl, protoFileForWebTmplRaw},
		{"protoFileForSimpleWeb", &protoFileForSimpleWebTmpl, protoFileForSimpleWebTmplRaw},
		{"protoMessageCreate", &protoMessageCreateTmpl, protoMessageCreateTmplRaw},
		{"protoMessageUpdate", &protoMessageUpdateTmpl, protoMessageUpdateTmplRaw},
		{"protoMessageDetail", &protoMessageDetailTmpl, protoMessageDetailTmplRaw},
		{"serviceCreateStruct", &serviceCreateStructTmpl, serviceCreateStructTmplRaw},
		{"serviceUpdateStruct", &serviceUpdateStructTmpl, serviceUpdateStructTmplRaw},
		{"serviceStruct", &serviceStructTmpl, serviceStructTmplRaw},
	}

	for _, config := range templates {
		tmpl, parseErr := parseTemplate(config.name, config.tmplRaw, errSum)
		if parseErr != nil {
			errSum = parseErr
		} else {
			*config.raw = tmpl
		}
	}

	return errSum
}

func initTemplate() {
	tmplParseOnce.Do(func() {
		if err := parseAllTemplates(); err != nil {
			panic(err)
		}
	})
}
