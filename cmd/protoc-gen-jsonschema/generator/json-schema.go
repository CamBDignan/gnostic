// Copyright 2021 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package generator

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"

	"github.com/google/gnostic/jsonschema"
)

var (
	typeString  = "string"
	typeNumber  = "number"
	typeInteger = "integer"
	typeBoolean = "boolean"
	typeObject  = "object"
	typeArray   = "array"
	typeNull    = "null"

	formatDate     = "date"
	formatDateTime = "date-time"
	formatEnum     = "enum"
	formatBytes    = "bytes"

	emptyString  = ""
	emptyInt64   = int64(0)
	emptyFloat64 = 0.0
	emptyBoolean = false
	emptyArray   = []*yaml.Node{}
)

func init() {
	log.SetFlags(log.Ltime | log.Lshortfile)
}

type Configuration struct {
	BaseURL  *string
	Version  *string
	Naming   *string
	EnumType *string
}

// JSONSchemaGenerator holds internal state needed to generate the JSON Schema documents for a transcoded Protocol Buffer service.
type JSONSchemaGenerator struct {
	conf   Configuration
	plugin *protogen.Plugin

	linterRulePattern *regexp.Regexp
}

// NewJSONSchemaGenerator creates a new generator for a protoc plugin invocation.
func NewJSONSchemaGenerator(plugin *protogen.Plugin, conf Configuration) *JSONSchemaGenerator {
	baseURL := *conf.BaseURL
	if len(baseURL) > 0 && baseURL[len(baseURL)-1:] != "/" {
		baseURL += "/"
	}
	conf.BaseURL = &baseURL

	return &JSONSchemaGenerator{
		conf:   conf,
		plugin: plugin,

		linterRulePattern: regexp.MustCompile(`\(-- .* --\)`),
	}
}

// Run runs the generator.
func (g *JSONSchemaGenerator) Run() error {
	for _, file := range g.plugin.Files {
		if file.Generate {
			schemas := g.buildSchemasFromMessages(file.Messages)
			for _, schema := range schemas {
				outputFile := g.plugin.NewGeneratedFile(fmt.Sprintf("%s.json", schema.Name), "")
				outputFile.Write([]byte(schema.Value.JSONString()))
			}
		}
	}

	return nil
}

// filterCommentString removes line breaks and linter rules from comments.
func (g *JSONSchemaGenerator) filterCommentString(c protogen.Comments, removeNewLines bool) string {
	comment := string(c)
	if removeNewLines {
		comment = strings.Replace(comment, "\n", "", -1)
	}
	comment = g.linterRulePattern.ReplaceAllString(comment, "")
	return strings.TrimSpace(comment)
}

func (g *JSONSchemaGenerator) formatMessageNameString(name string) string {
	if *g.conf.Naming == "proto" {
		return name
	}

	if len(name) > 1 {
		return strings.ToUpper(name[0:1]) + name[1:]
	}

	if len(name) == 1 {
		return strings.ToLower(name)
	}

	return name
}

func (g *JSONSchemaGenerator) formatOneofFieldName(oneof *protogen.Oneof) string {
	if *g.conf.Naming == "proto" {
		return string(oneof.Desc.Name())
	}

	name := oneof.GoName
	if len(name) > 1 {
		return strings.ToLower(name[0:1]) + name[1:]
	}

	if len(name) == 1 {
		return strings.ToLower(name)
	}

	return name
}

func (g *JSONSchemaGenerator) formatFieldName(field *protogen.Field) string {
	if *g.conf.Naming == "proto" {
		return string(field.Desc.Name())
	}

	return field.Desc.JSONName()
}

// messageDefinitionName builds the full schema definition name of a message.
func messageDefinitionName(desc protoreflect.MessageDescriptor) string {
	name := string(desc.Name())

	pkg := string(desc.ParentFile().Package())
	parentName := desc.Parent().FullName()
	if len(parentName) > len(pkg) {
		parentName = parentName[len(pkg)+1:]
		name = fmt.Sprintf("%s.%s", parentName, name)
	}

	return strings.Replace(name, ".", "_", -1)
}

func (g *JSONSchemaGenerator) schemaOrReferenceForType(desc protoreflect.MessageDescriptor) *jsonschema.Schema {
	// Create the full typeName
	typeName := fmt.Sprintf(".%s.%s", desc.ParentFile().Package(), desc.Name())

	switch typeName {

	case ".google.protobuf.Timestamp":
		// Timestamps are serialized as strings
		return &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeString}, Format: &formatDateTime}

	case ".google.type.Date":
		// Dates are serialized as strings
		return &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeString}, Format: &formatDate}

	case ".google.type.DateTime":
		// DateTimes are serialized as strings
		return &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeString}, Format: &formatDateTime}

	case ".google.protobuf.Struct":
		// Struct is equivalent to a JSON object
		return &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeObject}}

	case ".google.protobuf.Value":
		// Value is equivalent to any JSON value except null
		return &jsonschema.Schema{
			Type: &jsonschema.StringOrStringArray{
				StringArray: &[]string{typeString, typeNumber, typeInteger, typeBoolean, typeObject, typeArray},
			},
		}

	case ".google.protobuf.Empty":
		// Empty is close to JSON undefined than null, so ignore this field
		return nil
	}

	typeName = messageDefinitionName(desc)
	ref := g.formatMessageNameString(typeName) + ".json"
	return &jsonschema.Schema{Ref: &ref}
}

func (g *JSONSchemaGenerator) schemaOrReferenceForField(field protoreflect.FieldDescriptor, definitions *[]*jsonschema.NamedSchema) *jsonschema.Schema {
	if field.IsMap() {
		typ := "object"
		return &jsonschema.Schema{
			Type: &jsonschema.StringOrStringArray{String: &typ},
			AdditionalProperties: &jsonschema.SchemaOrBoolean{
				Schema: g.schemaOrReferenceForField(field.MapValue(), definitions),
			},
		}
	}

	var kindSchema *jsonschema.Schema

	kind := field.Kind()

	switch kind {

	case protoreflect.MessageKind:
		kindSchema = g.schemaOrReferenceForType(field.Message())
		if kindSchema == nil {
			return nil
		}

	case protoreflect.StringKind:
		kindSchema = &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeString}, Default: &jsonschema.DefaultValue{StringValue: &emptyString}}

	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Fixed64Kind:
		format := kind.String()
		kindSchema = &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeInteger}, Format: &format, Default: &jsonschema.DefaultValue{Int64Value: &emptyInt64}}

	case protoreflect.EnumKind:
		kindSchema = &jsonschema.Schema{Format: &formatEnum}
		if g.conf.EnumType != nil && *g.conf.EnumType == typeString {
			kindSchema.Type = &jsonschema.StringOrStringArray{String: &typeString}
			kindSchema.Enumeration = &[]jsonschema.SchemaEnumValue{}
			for i := 0; i < field.Enum().Values().Len(); i++ {
				name := string(field.Enum().Values().Get(i).Name())
				*kindSchema.Enumeration = append(*kindSchema.Enumeration, jsonschema.SchemaEnumValue{String: &name})
				if i == 0 {
					kindSchema.Default = &jsonschema.DefaultValue{StringValue: &name}
				}
			}
		} else {
			kindSchema.Type = &jsonschema.StringOrStringArray{String: &typeInteger}
			kindSchema.Default = &jsonschema.DefaultValue{Int64Value: &emptyInt64}
		}

	case protoreflect.BoolKind:
		kindSchema = &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeBoolean}, Default: &jsonschema.DefaultValue{BooleanValue: &emptyBoolean}}

	case protoreflect.FloatKind, protoreflect.DoubleKind:
		format := kind.String()
		kindSchema = &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeNumber}, Format: &format, Default: &jsonschema.DefaultValue{Float64Value: &emptyFloat64}}

	case protoreflect.BytesKind:
		kindSchema = &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeString}, Format: &formatBytes, Default: &jsonschema.DefaultValue{StringValue: &emptyString}}

	default:
		log.Printf("(TODO) Unsupported field type: %+v", field.Message().FullName())
	}

	if field.IsList() {
		typ := "array"
		return &jsonschema.Schema{
			Type: &jsonschema.StringOrStringArray{String: &typ},
			Items: &jsonschema.SchemaOrSchemaArray{
				Schema: kindSchema,
			},
			Default: &jsonschema.DefaultValue{ArrayValue: emptyArray},
		}
	}

	return kindSchema
}

func (g *JSONSchemaGenerator) namedSchemaForField(field *protogen.Field, schema *jsonschema.NamedSchema, isValueProp bool) *jsonschema.NamedSchema {
	// The field is either described by a reference or a schema.
	fieldSchema := g.schemaOrReferenceForField(field.Desc, schema.Value.Definitions)
	if fieldSchema == nil {
		return nil
	}

	// Handle readonly and writeonly properties, if the schema version can handle it.
	if getSchemaVersion(schema.Value) >= "07" {
		t := true
		// Check the field annotations to see if this is a readonly field.
		extension := proto.GetExtension(field.Desc.Options(), annotations.E_FieldBehavior)
		if extension != nil {
			switch v := extension.(type) {
			case []annotations.FieldBehavior:
				for _, vv := range v {
					if vv == annotations.FieldBehavior_OUTPUT_ONLY {
						fieldSchema.ReadOnly = &t
					} else if vv == annotations.FieldBehavior_INPUT_ONLY {
						fieldSchema.WriteOnly = &t
					}
				}
			default:
				log.Printf("unsupported extension type %T", extension)
			}
		}
	}

	fieldName := "value"
	if !isValueProp {
		fieldName = g.formatFieldName(field)
	}

	// Do not add title for ref values
	if fieldSchema.Ref == nil {
		fieldSchema.Title = &fieldName
	}

	// Get the field description from the comments.
	description := g.filterCommentString(field.Comments.Leading, true)
	if description != "" {
		// Note: Description will be ignored if $ref is set, but is still useful
		fieldSchema.Description = &description
	}

	return &jsonschema.NamedSchema{
		Name:  fieldName,
		Value: fieldSchema,
	}
}

func (g *JSONSchemaGenerator) setupSchemaForMessage(schemaName string, comments protogen.Comments) *jsonschema.NamedSchema {
	typ := "object"
	id := fmt.Sprintf("%s%s.json", *g.conf.BaseURL, schemaName)

	schema := &jsonschema.NamedSchema{
		Name: schemaName,
		Value: &jsonschema.Schema{
			Schema:     g.conf.Version,
			ID:         &id,
			Type:       &jsonschema.StringOrStringArray{String: &typ},
			Title:      &schemaName,
			Properties: &[]*jsonschema.NamedSchema{},
		},
	}

	description := g.filterCommentString(comments, true)
	if description != "" {
		schema.Value.Description = &description
	}

	return schema
}

func (g *JSONSchemaGenerator) buildKindProperty(propertyValue string) *jsonschema.NamedSchema {
	kind := "kind"
	kindProperty := &jsonschema.NamedSchema{
		Name: kind,
		Value: &jsonschema.Schema{
			Title:       &kind,
			Type:        &jsonschema.StringOrStringArray{String: &typeString},
			Enumeration: &[]jsonschema.SchemaEnumValue{},
			Default:     &jsonschema.DefaultValue{StringValue: &propertyValue},
		},
	}
	*kindProperty.Value.Enumeration = append(
		*kindProperty.Value.Enumeration,
		jsonschema.SchemaEnumValue{String: &propertyValue},
	)
	return kindProperty
}

func (g *JSONSchemaGenerator) addOneofFieldsToSchema(oneofs []*protogen.Oneof, schema *jsonschema.NamedSchema) {
	if oneofs == nil {
		return
	}

	for _, oneOfProto := range oneofs {
		oneOfSchema := jsonschema.Schema{
			OneOf:   &[]*jsonschema.Schema{},
			Default: &jsonschema.DefaultValue{NullTag: true},
		}

		*oneOfSchema.OneOf = append(*oneOfSchema.OneOf, &jsonschema.Schema{Type: &jsonschema.StringOrStringArray{String: &typeNull}})

		for _, fieldProto := range oneOfProto.Fields {
			ref := schema.Name + "_" + fieldProto.GoName
			oneofFieldSchema := &jsonschema.NamedSchema{
				Name: ref,
				Value: &jsonschema.Schema{
					Type:       &jsonschema.StringOrStringArray{String: &typeObject},
					Title:      &ref,
					Properties: &[]*jsonschema.NamedSchema{},
				},
			}
			kindProperty := g.buildKindProperty(string(fieldProto.Desc.Name()))
			actualProperty := g.namedSchemaForField(fieldProto, schema, true)
			if actualProperty == nil {
				continue
			}

			*oneofFieldSchema.Value.Properties = append(
				*oneofFieldSchema.Value.Properties,
				kindProperty,
				actualProperty,
			)

			if schema.Value.Definitions == nil {
				schema.Value.Definitions = &[]*jsonschema.NamedSchema{}
			}
			*schema.Value.Definitions = append(*schema.Value.Definitions, oneofFieldSchema)

			definitionsRef := "#/definitions/" + ref
			*oneOfSchema.OneOf = append(*oneOfSchema.OneOf, &jsonschema.Schema{Ref: &definitionsRef})
		}

		*schema.Value.Properties = append(
			*schema.Value.Properties,
			&jsonschema.NamedSchema{
				Name:  g.formatOneofFieldName(oneOfProto),
				Value: &oneOfSchema,
			},
		)
	}
}

// buildSchemasFromMessages creates a schema for each message.
func (g *JSONSchemaGenerator) buildSchemasFromMessages(messages []*protogen.Message) []*jsonschema.NamedSchema {
	schemas := []*jsonschema.NamedSchema{}

	// For each message, generate a schema.
	for _, message := range messages {
		schemaName := messageDefinitionName(message.Desc)
		schema := g.setupSchemaForMessage(schemaName, message.Comments.Leading)

		// Any embedded messages will be created as new schemas
		if message.Messages != nil {
			for _, subMessage := range message.Messages {
				subSchemas := g.buildSchemasFromMessages([]*protogen.Message{subMessage})
				schemas = append(schemas, subSchemas...)
			}
		}

		if message.Desc.IsMapEntry() {
			continue
		}

		g.addOneofFieldsToSchema(message.Oneofs, schema)

		for _, field := range message.Fields {
			if field.Oneof != nil {
				continue
			}

			namedSchema := g.namedSchemaForField(field, schema, false)
			if namedSchema == nil {
				continue
			}

			*schema.Value.Properties = append(
				*schema.Value.Properties,
				namedSchema,
			)
		}

		schemas = append(schemas, schema)
	}

	return schemas
}

var reSchemaVersion = regexp.MustCompile(`https*://json-schema.org/draft[/-]([^/]+)/schema`)

func getSchemaVersion(schema *jsonschema.Schema) string {
	schemaSchema := *schema.Schema
	matches := reSchemaVersion.FindStringSubmatch(schemaSchema)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}
