// Copyright 2016 Google Inc. All Rights Reserved.
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

// THIS FILE IS AUTOMATICALLY GENERATED.

package main

import (
	"github.com/golang/protobuf/proto"
	"github.com/googleapis/gnostic/compiler"
	"github.com/googleapis/gnostic/extensions"
	"github.com/googleapis/gnostic/extensions/sample/generated/openapi_extensions_insourcetech/proto"
	"gopkg.in/yaml.v2"
)

func handleExtension(extensionName string, info yaml.MapSlice) (bool, proto.Message, error) {
	switch extensionName {
	// All supported extensions

	case "x-insourcetech-book":
		newObject, err := insourcetech.NewInSourceBook(info, compiler.NewContext("$root", nil))
		return true, newObject, err
	case "x-insourcetech-shelve":
		newObject, err := insourcetech.NewInSourceShelve(info, compiler.NewContext("$root", nil))
		return true, newObject, err
	default:
		return false, nil, nil
	}
}

func main() {
	openapiextension_v1.ProcessExtension(handleExtension)
}
